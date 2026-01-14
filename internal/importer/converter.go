package importer

import (
	"fmt"

	"github.com/bobbyrathoree/kbox/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// Convert converts parsed K8s resources to a kbox AppConfig
func Convert(resources *K8sResources) (*config.AppConfig, error) {
	if !resources.HasDeployment() {
		return nil, fmt.Errorf("no Deployment found in input files\n  → kbox import requires at least one Deployment")
	}

	// Use the first deployment as the primary app
	dep := resources.Deployments[0]

	cfg := &config.AppConfig{
		APIVersion: config.DefaultAPIVersion,
		Kind:       config.DefaultKind,
		Metadata: config.Metadata{
			Name: dep.Name,
		},
		Spec: config.AppSpec{},
	}

	// Set namespace if not default
	if dep.Namespace != "" && dep.Namespace != "default" {
		cfg.Metadata.Namespace = dep.Namespace
	}

	// Extract from deployment
	if err := extractFromDeployment(dep, cfg); err != nil {
		return nil, err
	}

	// Extract from services
	extractFromServices(resources.Services, cfg)

	// Extract from configmaps (as env vars)
	extractFromConfigMaps(resources.ConfigMaps, cfg)

	// Apply defaults to fill in any missing values
	cfg.WithDefaults()

	return cfg, nil
}

func extractFromDeployment(dep *appsv1.Deployment, cfg *config.AppConfig) error {
	// Replicas
	if dep.Spec.Replicas != nil {
		cfg.Spec.Replicas = int(*dep.Spec.Replicas)
	}

	// We need at least one container
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		return fmt.Errorf("deployment has no containers")
	}

	container := dep.Spec.Template.Spec.Containers[0]

	// Image
	cfg.Spec.Image = container.Image

	// Port - from container ports
	if len(container.Ports) > 0 {
		cfg.Spec.Port = int(container.Ports[0].ContainerPort)
	}

	// Command and Args
	if len(container.Command) > 0 {
		cfg.Spec.Command = container.Command
	}
	if len(container.Args) > 0 {
		cfg.Spec.Args = container.Args
	}

	// Environment variables (direct values only)
	if len(container.Env) > 0 {
		cfg.Spec.Env = make(map[string]string)
		for _, env := range container.Env {
			// Only import direct values, not valueFrom references
			if env.Value != "" && env.ValueFrom == nil {
				cfg.Spec.Env[env.Name] = env.Value
			}
		}
		// Remove if empty
		if len(cfg.Spec.Env) == 0 {
			cfg.Spec.Env = nil
		}
	}

	// Resources
	if container.Resources.Requests != nil || container.Resources.Limits != nil {
		cfg.Spec.Resources = &config.ResourceConfig{}

		if container.Resources.Requests != nil {
			if mem := container.Resources.Requests.Memory(); mem != nil && !mem.IsZero() {
				cfg.Spec.Resources.Memory = mem.String()
			}
			if cpu := container.Resources.Requests.Cpu(); cpu != nil && !cpu.IsZero() {
				cfg.Spec.Resources.CPU = cpu.String()
			}
		}

		if container.Resources.Limits != nil {
			if mem := container.Resources.Limits.Memory(); mem != nil && !mem.IsZero() {
				cfg.Spec.Resources.MemoryLimit = mem.String()
			}
			if cpu := container.Resources.Limits.Cpu(); cpu != nil && !cpu.IsZero() {
				cfg.Spec.Resources.CPULimit = cpu.String()
			}
		}

		// Remove resources if all empty
		if cfg.Spec.Resources.Memory == "" && cfg.Spec.Resources.CPU == "" &&
			cfg.Spec.Resources.MemoryLimit == "" && cfg.Spec.Resources.CPULimit == "" {
			cfg.Spec.Resources = nil
		}
	}

	// Health check from liveness probe
	if container.LivenessProbe != nil && container.LivenessProbe.HTTPGet != nil {
		cfg.Spec.HealthCheck = container.LivenessProbe.HTTPGet.Path
	} else if container.ReadinessProbe != nil && container.ReadinessProbe.HTTPGet != nil {
		cfg.Spec.HealthCheck = container.ReadinessProbe.HTTPGet.Path
	}

	return nil
}

func extractFromServices(services []*corev1.Service, cfg *config.AppConfig) {
	if len(services) == 0 {
		return
	}

	// Find a service that matches the deployment name
	var matchingSvc *corev1.Service
	for _, svc := range services {
		if svc.Name == cfg.Metadata.Name {
			matchingSvc = svc
			break
		}
	}

	// If no exact match, use the first service
	if matchingSvc == nil {
		matchingSvc = services[0]
	}

	// Extract service type
	if matchingSvc.Spec.Type != "" && matchingSvc.Spec.Type != corev1.ServiceTypeClusterIP {
		cfg.Spec.Service = &config.ServiceConfig{
			Type: string(matchingSvc.Spec.Type),
		}
	}

	// If service has a different port than the container port, record it
	if len(matchingSvc.Spec.Ports) > 0 {
		svcPort := int(matchingSvc.Spec.Ports[0].Port)
		if cfg.Spec.Port == 0 {
			cfg.Spec.Port = svcPort
		}
	}
}

func extractFromConfigMaps(configMaps []*corev1.ConfigMap, cfg *config.AppConfig) {
	if len(configMaps) == 0 {
		return
	}

	// Merge all ConfigMap data into env vars
	if cfg.Spec.Env == nil {
		cfg.Spec.Env = make(map[string]string)
	}

	for _, cm := range configMaps {
		for k, v := range cm.Data {
			// Only add if not already set from deployment
			if _, exists := cfg.Spec.Env[k]; !exists {
				cfg.Spec.Env[k] = v
			}
		}
	}

	// Remove if empty
	if len(cfg.Spec.Env) == 0 {
		cfg.Spec.Env = nil
	}
}

// ConvertMultiple converts resources that may contain multiple deployments
// into separate AppConfigs (for future multi-service support)
func ConvertMultiple(resources *K8sResources) ([]*config.AppConfig, error) {
	if !resources.HasDeployment() {
		return nil, fmt.Errorf("no Deployment found in input files")
	}

	var configs []*config.AppConfig

	for _, dep := range resources.Deployments {
		cfg := &config.AppConfig{
			APIVersion: config.DefaultAPIVersion,
			Kind:       config.DefaultKind,
			Metadata: config.Metadata{
				Name: dep.Name,
			},
			Spec: config.AppSpec{},
		}

		if dep.Namespace != "" && dep.Namespace != "default" {
			cfg.Metadata.Namespace = dep.Namespace
		}

		if err := extractFromDeployment(dep, cfg); err != nil {
			return nil, fmt.Errorf("deployment %s: %w", dep.Name, err)
		}

		// Find matching service
		for _, svc := range resources.Services {
			if svc.Name == dep.Name {
				extractFromServices([]*corev1.Service{svc}, cfg)
				break
			}
		}

		cfg.WithDefaults()
		configs = append(configs, cfg)
	}

	return configs, nil
}
