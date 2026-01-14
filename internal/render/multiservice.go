package render

import (
	"fmt"

	"github.com/bobbyrathoree/kbox/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// MultiServiceRenderer renders multi-service configurations
type MultiServiceRenderer struct {
	config *config.MultiServiceConfig
}

// NewMultiService creates a new multi-service renderer
func NewMultiService(cfg *config.MultiServiceConfig) *MultiServiceRenderer {
	return &MultiServiceRenderer{config: cfg}
}

// Render renders all services in dependency order
func (r *MultiServiceRenderer) Render() (*Bundle, error) {
	bundle := &Bundle{}

	// Get services in dependency order
	order := r.config.ServiceOrder()

	// Render each service
	for _, serviceName := range order {
		// Convert to AppConfig for rendering
		appCfg, err := r.config.ToAppConfig(serviceName)
		if err != nil {
			return nil, fmt.Errorf("failed to convert service %s: %w", serviceName, err)
		}

		// Use standard renderer
		renderer := New(appCfg)

		// Render deployment
		deployment, err := renderer.RenderDeployment()
		if err != nil {
			return nil, fmt.Errorf("failed to render deployment for %s: %w", serviceName, err)
		}

		// Add service discovery environment variables
		r.addServiceDiscoveryEnv(deployment, serviceName)

		bundle.Deployments = append(bundle.Deployments, deployment)

		// Render service
		service, err := renderer.RenderService()
		if err != nil {
			return nil, fmt.Errorf("failed to render service for %s: %w", serviceName, err)
		}
		bundle.Services = append(bundle.Services, service)

		// Render configmap if service has env vars
		svc := r.config.Services[serviceName]
		if len(svc.Env) > 0 {
			cm, err := renderer.RenderConfigMap()
			if err != nil {
				return nil, fmt.Errorf("failed to render configmap for %s: %w", serviceName, err)
			}
			bundle.ConfigMaps = append(bundle.ConfigMaps, cm)
		}
	}

	// Set Deployment to first deployment for backward compatibility
	if len(bundle.Deployments) > 0 {
		bundle.Deployment = bundle.Deployments[0]
	}

	return bundle, nil
}

// addServiceDiscoveryEnv adds environment variables for service discovery
func (r *MultiServiceRenderer) addServiceDiscoveryEnv(deployment *appsv1.Deployment, currentService string) {
	// Build service URLs for all services
	serviceURLs := make(map[string]string)
	for name, svc := range r.config.Services {
		// Use K8s internal DNS format: service-name.namespace.svc.cluster.local
		// But for same namespace, we can just use service-name:port
		serviceName := fmt.Sprintf("%s-%s", r.config.Metadata.Name, name)
		serviceURLs[name] = fmt.Sprintf("http://%s:%d", serviceName, svc.Port)
	}

	// Add environment variables to all containers
	for i := range deployment.Spec.Template.Spec.Containers {
		container := &deployment.Spec.Template.Spec.Containers[i]

		// Add service URLs for dependent services
		svc := r.config.Services[currentService]
		for _, depName := range svc.DependsOn {
			envName := fmt.Sprintf("%s_URL", toEnvName(depName))
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  envName,
				Value: serviceURLs[depName],
			})
		}
	}
}

// toEnvName converts a service name to an environment variable name
func toEnvName(name string) string {
	result := make([]byte, len(name))
	for i, c := range name {
		if c == '-' {
			result[i] = '_'
		} else if c >= 'a' && c <= 'z' {
			result[i] = byte(c - 32) // to uppercase
		} else {
			result[i] = byte(c)
		}
	}
	return string(result)
}

// Namespace returns the target namespace
func (r *MultiServiceRenderer) Namespace() string {
	if r.config.Metadata.Namespace != "" {
		return r.config.Metadata.Namespace
	}
	return "default"
}

// Labels returns standard labels for the multi-service app
func (r *MultiServiceRenderer) Labels() map[string]string {
	return map[string]string{
		"app":                          r.config.Metadata.Name,
		"app.kubernetes.io/name":       r.config.Metadata.Name,
		"app.kubernetes.io/managed-by": "kbox",
		"kbox.dev/multi-service":       "true",
	}
}
