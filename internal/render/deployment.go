package render

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// RenderDeployment renders a Kubernetes Deployment from the config
func (r *Renderer) RenderDeployment() (*appsv1.Deployment, error) {
	cfg := r.config
	name := cfg.Metadata.Name

	replicas := int32(cfg.Spec.Replicas)
	if replicas == 0 {
		replicas = 1
	}

	// Build container
	container := corev1.Container{
		Name:  name,
		Image: cfg.Spec.Image,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: int32(cfg.Spec.Port),
				Protocol:      corev1.ProtocolTCP,
			},
		},
	}

	// Add command/args if specified
	if len(cfg.Spec.Command) > 0 {
		container.Command = cfg.Spec.Command
	}
	if len(cfg.Spec.Args) > 0 {
		container.Args = cfg.Spec.Args
	}

	// Add environment variables
	container.Env = r.renderEnvVars()

	// Add envFrom for secrets
	container.EnvFrom = r.renderEnvFrom()

	// Add resource requirements
	container.Resources = r.renderResources()

	// Add health probes if healthCheck is specified
	if cfg.Spec.HealthCheck != "" {
		probe := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: cfg.Spec.HealthCheck,
					Port: intstr.FromInt32(int32(cfg.Spec.Port)),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		}
		container.LivenessProbe = probe
		container.ReadinessProbe = probe.DeepCopy()
		container.ReadinessProbe.InitialDelaySeconds = 3
	}

	// Build deployment
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.Namespace(),
			Labels:    r.Labels(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.Selector(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: r.Labels(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
					MaxSurge:       &intstr.IntOrString{Type: intstr.String, StrVal: "25%"},
				},
			},
		},
	}

	return deployment, nil
}

func (r *Renderer) renderEnvVars() []corev1.EnvVar {
	var envVars []corev1.EnvVar

	// Add env vars from config
	for k, v := range r.config.Spec.Env {
		envVars = append(envVars, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
	}

	// Sort for deterministic output
	sortEnvVars(envVars)

	return envVars
}

func (r *Renderer) renderEnvFrom() []corev1.EnvFromSource {
	var envFrom []corev1.EnvFromSource

	// Add secret reference for .env file if configured
	if r.config.Spec.Secrets != nil && r.config.Spec.Secrets.FromEnvFile != "" {
		secretName := r.config.Metadata.Name + "-secrets"
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
			},
		})
	}

	// Add secret reference for SOPS files if configured
	if r.config.Spec.Secrets != nil && len(r.config.Spec.Secrets.FromSops) > 0 {
		secretName := r.config.Metadata.Name + "-sops-secrets"
		envFrom = append(envFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
			},
		})
	}

	return envFrom
}

func (r *Renderer) renderResources() corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}

	cfg := r.config.Spec.Resources
	if cfg == nil {
		// Sensible defaults
		resources.Requests[corev1.ResourceMemory] = resource.MustParse("128Mi")
		resources.Requests[corev1.ResourceCPU] = resource.MustParse("100m")
		resources.Limits[corev1.ResourceMemory] = resource.MustParse("256Mi")
		resources.Limits[corev1.ResourceCPU] = resource.MustParse("200m")
		return resources
	}

	// Memory
	if cfg.Memory != "" {
		mem := resource.MustParse(cfg.Memory)
		resources.Requests[corev1.ResourceMemory] = mem
		// Default limit to 2x request
		if cfg.MemoryLimit != "" {
			resources.Limits[corev1.ResourceMemory] = resource.MustParse(cfg.MemoryLimit)
		} else {
			memLimit := mem.DeepCopy()
			memLimit.Add(mem)
			resources.Limits[corev1.ResourceMemory] = memLimit
		}
	}

	// CPU
	if cfg.CPU != "" {
		cpu := resource.MustParse(cfg.CPU)
		resources.Requests[corev1.ResourceCPU] = cpu
		// Default limit to 2x request
		if cfg.CPULimit != "" {
			resources.Limits[corev1.ResourceCPU] = resource.MustParse(cfg.CPULimit)
		} else {
			cpuLimit := cpu.DeepCopy()
			cpuLimit.Add(cpu)
			resources.Limits[corev1.ResourceCPU] = cpuLimit
		}
	}

	return resources
}

func sortEnvVars(envVars []corev1.EnvVar) {
	// Simple bubble sort for deterministic output
	for i := 0; i < len(envVars); i++ {
		for j := i + 1; j < len(envVars); j++ {
			if envVars[i].Name > envVars[j].Name {
				envVars[i], envVars[j] = envVars[j], envVars[i]
			}
		}
	}
}

// ImageWithTag returns the image with a specific tag
func ImageWithTag(baseImage, tag string) string {
	// Find the last colon that's not part of a port
	lastColon := -1
	for i := len(baseImage) - 1; i >= 0; i-- {
		if baseImage[i] == ':' {
			// Check if this is a tag separator (not a port)
			// Port would have all digits after it
			if i < len(baseImage)-1 {
				lastColon = i
				break
			}
		}
		if baseImage[i] == '/' {
			break
		}
	}

	if lastColon > 0 {
		// Replace existing tag
		return fmt.Sprintf("%s:%s", baseImage[:lastColon], tag)
	}
	// Add tag
	return fmt.Sprintf("%s:%s", baseImage, tag)
}
