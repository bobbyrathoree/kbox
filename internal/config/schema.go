package config

// AppConfig represents the full kbox.yaml configuration
type AppConfig struct {
	APIVersion   string            `yaml:"apiVersion" json:"apiVersion"`
	Kind         string            `yaml:"kind" json:"kind"`
	Metadata     Metadata          `yaml:"metadata" json:"metadata"`
	Spec         AppSpec           `yaml:"spec" json:"spec"`
	Environments map[string]EnvOverride `yaml:"environments,omitempty" json:"environments,omitempty"`
}

// Metadata contains app identification
type Metadata struct {
	Name      string            `yaml:"name" json:"name"`
	Namespace string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Labels    map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// AppSpec defines the application specification
type AppSpec struct {
	// Image is the container image (required unless using build)
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Build configuration for building images
	Build *BuildConfig `yaml:"build,omitempty" json:"build,omitempty"`

	// Port the application listens on (default: 8080)
	Port int `yaml:"port,omitempty" json:"port,omitempty"`

	// Replicas count (default: 1)
	Replicas int `yaml:"replicas,omitempty" json:"replicas,omitempty"`

	// Env variables
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// Secrets configuration
	Secrets *SecretsConfig `yaml:"secrets,omitempty" json:"secrets,omitempty"`

	// HealthCheck path for liveness/readiness probes
	HealthCheck string `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`

	// Resources requests and limits
	Resources *ResourceConfig `yaml:"resources,omitempty" json:"resources,omitempty"`

	// Service configuration
	Service *ServiceConfig `yaml:"service,omitempty" json:"service,omitempty"`

	// Ingress configuration
	Ingress *IngressConfig `yaml:"ingress,omitempty" json:"ingress,omitempty"`

	// Include raw manifest files
	Include []string `yaml:"include,omitempty" json:"include,omitempty"`

	// Overrides for generated resources
	Overrides *OverrideConfig `yaml:"overrides,omitempty" json:"overrides,omitempty"`

	// Command override
	Command []string `yaml:"command,omitempty" json:"command,omitempty"`

	// Args override
	Args []string `yaml:"args,omitempty" json:"args,omitempty"`
}

// BuildConfig defines how to build the image
type BuildConfig struct {
	// Context is the build context path (default: .)
	Context string `yaml:"context,omitempty" json:"context,omitempty"`

	// Dockerfile path (default: Dockerfile)
	Dockerfile string `yaml:"dockerfile,omitempty" json:"dockerfile,omitempty"`

	// Target for multi-stage builds
	Target string `yaml:"target,omitempty" json:"target,omitempty"`

	// Args for build-time variables
	Args map[string]string `yaml:"args,omitempty" json:"args,omitempty"`
}

// SecretsConfig defines secret sources
type SecretsConfig struct {
	// FromEnvFile loads secrets from a .env file (simple, v0.1)
	FromEnvFile string `yaml:"fromEnvFile,omitempty" json:"fromEnvFile,omitempty"`

	// FromSops loads secrets from sops-encrypted files (v0.2+)
	FromSops []string `yaml:"fromSops,omitempty" json:"fromSops,omitempty"`
}

// ResourceConfig defines resource requests/limits
type ResourceConfig struct {
	// Memory request/limit (e.g., "256Mi")
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"`

	// CPU request/limit (e.g., "100m")
	CPU string `yaml:"cpu,omitempty" json:"cpu,omitempty"`

	// MemoryLimit if different from request
	MemoryLimit string `yaml:"memoryLimit,omitempty" json:"memoryLimit,omitempty"`

	// CPULimit if different from request
	CPULimit string `yaml:"cpuLimit,omitempty" json:"cpuLimit,omitempty"`
}

// ServiceConfig defines service configuration
type ServiceConfig struct {
	// Type of service (ClusterIP, NodePort, LoadBalancer)
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Port to expose (default: same as app port)
	Port int `yaml:"port,omitempty" json:"port,omitempty"`

	// TargetPort on the container (default: app port)
	TargetPort int `yaml:"targetPort,omitempty" json:"targetPort,omitempty"`
}

// IngressConfig defines ingress configuration
type IngressConfig struct {
	// Enabled creates an ingress resource
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// Host for the ingress rule
	Host string `yaml:"host,omitempty" json:"host,omitempty"`

	// Path prefix (default: /)
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// TLS configuration
	TLS *TLSConfig `yaml:"tls,omitempty" json:"tls,omitempty"`

	// Annotations for the ingress
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// TLSConfig for ingress TLS
type TLSConfig struct {
	// Enabled enables TLS
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// SecretName for TLS certificate
	SecretName string `yaml:"secretName,omitempty" json:"secretName,omitempty"`
}

// OverrideConfig allows overriding generated resources
type OverrideConfig struct {
	// Deployment overrides merged into generated deployment
	Deployment map[string]interface{} `yaml:"deployment,omitempty" json:"deployment,omitempty"`

	// Service overrides merged into generated service
	Service map[string]interface{} `yaml:"service,omitempty" json:"service,omitempty"`
}

// EnvOverride defines environment-specific overrides
type EnvOverride struct {
	// Replicas override
	Replicas *int `yaml:"replicas,omitempty" json:"replicas,omitempty"`

	// Env variables to add/override
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// Resources override
	Resources *ResourceConfig `yaml:"resources,omitempty" json:"resources,omitempty"`

	// Image override (e.g., for different registries per env)
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Ingress override
	Ingress *IngressConfig `yaml:"ingress,omitempty" json:"ingress,omitempty"`
}

// MultiServiceConfig represents a multi-service kbox.yaml configuration
type MultiServiceConfig struct {
	APIVersion string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                 `yaml:"kind" json:"kind"` // "MultiApp"
	Metadata   Metadata               `yaml:"metadata" json:"metadata"`
	Services   map[string]ServiceSpec `yaml:"services" json:"services"`
}

// ServiceSpec defines a single service in a multi-service app
type ServiceSpec struct {
	// Build configuration for building images
	Build *BuildConfig `yaml:"build,omitempty" json:"build,omitempty"`

	// Image is the container image
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Port the service listens on
	Port int `yaml:"port,omitempty" json:"port,omitempty"`

	// Replicas count
	Replicas int `yaml:"replicas,omitempty" json:"replicas,omitempty"`

	// Env variables
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// DependsOn lists services this one depends on
	DependsOn []string `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`

	// HealthCheck path
	HealthCheck string `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`

	// Resources requests and limits
	Resources *ResourceConfig `yaml:"resources,omitempty" json:"resources,omitempty"`

	// Command override
	Command []string `yaml:"command,omitempty" json:"command,omitempty"`

	// Args override
	Args []string `yaml:"args,omitempty" json:"args,omitempty"`

	// Service configuration
	Service *ServiceConfig `yaml:"service,omitempty" json:"service,omitempty"`
}

// Defaults for the config
const (
	DefaultAPIVersion = "kbox.dev/v1"
	DefaultKind       = "App"
	MultiAppKind      = "MultiApp"
	DefaultPort       = 8080
	DefaultReplicas   = 1
)

// NewDefaultConfig creates a config with sensible defaults
func NewDefaultConfig(name string) *AppConfig {
	return &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata: Metadata{
			Name: name,
		},
		Spec: AppSpec{
			Port:     DefaultPort,
			Replicas: DefaultReplicas,
		},
	}
}

// WithDefaults applies default values to a config
func (c *AppConfig) WithDefaults() *AppConfig {
	if c.APIVersion == "" {
		c.APIVersion = DefaultAPIVersion
	}
	if c.Kind == "" {
		c.Kind = DefaultKind
	}
	if c.Spec.Port == 0 {
		c.Spec.Port = DefaultPort
	}
	if c.Spec.Replicas == 0 {
		c.Spec.Replicas = DefaultReplicas
	}
	return c
}

// ForEnvironment returns a config merged with environment-specific overrides
func (c *AppConfig) ForEnvironment(env string) *AppConfig {
	if env == "" || c.Environments == nil {
		return c
	}

	override, ok := c.Environments[env]
	if !ok {
		return c
	}

	// Create a copy
	result := *c

	// Apply overrides
	if override.Replicas != nil {
		result.Spec.Replicas = *override.Replicas
	}

	if override.Image != "" {
		result.Spec.Image = override.Image
	}

	if override.Resources != nil {
		result.Spec.Resources = override.Resources
	}

	if override.Ingress != nil {
		result.Spec.Ingress = override.Ingress
	}

	// Merge env vars
	if len(override.Env) > 0 {
		if result.Spec.Env == nil {
			result.Spec.Env = make(map[string]string)
		}
		for k, v := range override.Env {
			result.Spec.Env[k] = v
		}
	}

	return &result
}
