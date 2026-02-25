package config

import (
	"fmt"
	"sort"
	"strings"
)

// AppConfig represents the full kbox.yaml configuration
type AppConfig struct {
	APIVersion   string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind         string                 `yaml:"kind" json:"kind"`
	Metadata     Metadata               `yaml:"metadata" json:"metadata"`
	Spec         AppSpec                `yaml:"spec" json:"spec"`
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

	// Annotations to add to all resources (merged with resource-specific annotations)
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`

	// PodAnnotations to add to pod templates (e.g., prometheus.io/scrape)
	PodAnnotations map[string]string `yaml:"podAnnotations,omitempty" json:"podAnnotations,omitempty"`

	// ServiceAnnotations to add to services (e.g., AWS ALB annotations)
	ServiceAnnotations map[string]string `yaml:"serviceAnnotations,omitempty" json:"serviceAnnotations,omitempty"`

	// DeploymentAnnotations to add to deployments
	DeploymentAnnotations map[string]string `yaml:"deploymentAnnotations,omitempty" json:"deploymentAnnotations,omitempty"`

	// Labels to add to all resources (merged with default labels)
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`

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

	// Dependencies are managed database/cache services
	Dependencies []DependencyConfig `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`

	// Volumes for persistent storage, ephemeral storage, or config mounts
	Volumes []VolumeConfig `yaml:"volumes,omitempty" json:"volumes,omitempty"`

	// InitContainers run before the main container starts
	InitContainers []InitContainerConfig `yaml:"initContainers,omitempty" json:"initContainers,omitempty"`

	// Jobs for one-off tasks and scheduled jobs (CronJobs)
	Jobs []JobConfig `yaml:"jobs,omitempty" json:"jobs,omitempty"`

	// Autoscaling configuration for HorizontalPodAutoscaler
	Autoscaling *AutoscalingConfig `yaml:"autoscaling,omitempty" json:"autoscaling,omitempty"`

	// PDB configuration for PodDisruptionBudget
	PDB *PDBConfig `yaml:"pdb,omitempty" json:"pdb,omitempty"`

	// Metrics configuration for Prometheus ServiceMonitor
	Metrics *MetricsConfig `yaml:"metrics,omitempty" json:"metrics,omitempty"`

	// Tracing configuration for distributed tracing sidecar
	Tracing *TracingConfig `yaml:"tracing,omitempty" json:"tracing,omitempty"`

	// Security allows customization of security settings
	Security *SecurityConfig `yaml:"security,omitempty" json:"security,omitempty"`
}

// DependencyConfig defines a managed dependency like postgres or redis
type DependencyConfig struct {
	// Type is the dependency type (postgres, redis, mongodb, mysql)
	Type string `yaml:"type" json:"type"`

	// Version specifies the version (e.g., "15", "7")
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	// Storage size for persistent data (default: 1Gi)
	Storage string `yaml:"storage,omitempty" json:"storage,omitempty"`

	// Resources for the dependency container
	Resources *ResourceConfig `yaml:"resources,omitempty" json:"resources,omitempty"`
}

// VolumeConfig defines a volume mount for the app
type VolumeConfig struct {
	// Name of the volume (used for PVC name and volume reference)
	Name string `yaml:"name" json:"name"`

	// MountPath where the volume is mounted in the container
	MountPath string `yaml:"mountPath" json:"mountPath"`

	// Size creates a PersistentVolumeClaim with this size (e.g., "10Gi")
	Size string `yaml:"size,omitempty" json:"size,omitempty"`

	// EmptyDir creates an ephemeral volume (not persisted across restarts)
	EmptyDir bool `yaml:"emptyDir,omitempty" json:"emptyDir,omitempty"`

	// ConfigMap mounts a ConfigMap as a volume
	ConfigMap string `yaml:"configMap,omitempty" json:"configMap,omitempty"`

	// Secret mounts a Secret as a volume
	Secret string `yaml:"secret,omitempty" json:"secret,omitempty"`

	// SubPath mounts a specific key from ConfigMap/Secret
	SubPath string `yaml:"subPath,omitempty" json:"subPath,omitempty"`

	// ReadOnly mounts the volume as read-only
	ReadOnly bool `yaml:"readOnly,omitempty" json:"readOnly,omitempty"`
}

// InitContainerConfig defines an init container that runs before the main container
type InitContainerConfig struct {
	// Name of the init container
	Name string `yaml:"name" json:"name"`

	// Image for the init container (defaults to app image if not specified)
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Command to run
	Command []string `yaml:"command" json:"command"`

	// Args for the command
	Args []string `yaml:"args,omitempty" json:"args,omitempty"`

	// Env variables for the init container
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// AutoscalingConfig defines HPA settings
type AutoscalingConfig struct {
	Enabled              bool `yaml:"enabled" json:"enabled"`
	MinReplicas          int  `yaml:"minReplicas,omitempty" json:"minReplicas,omitempty"`
	MaxReplicas          int  `yaml:"maxReplicas" json:"maxReplicas"`
	TargetCPUUtilization int  `yaml:"targetCPUUtilization,omitempty" json:"targetCPUUtilization,omitempty"`
}

// PDBConfig defines PodDisruptionBudget settings
type PDBConfig struct {
	MinAvailable   string `yaml:"minAvailable,omitempty" json:"minAvailable,omitempty"`
	MaxUnavailable string `yaml:"maxUnavailable,omitempty" json:"maxUnavailable,omitempty"`
}

// MetricsConfig configures Prometheus metrics and ServiceMonitor generation
type MetricsConfig struct {
	// Enabled creates a ServiceMonitor for Prometheus scraping
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Path for metrics endpoint (default: /metrics)
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Port name to scrape (default: "http", uses app's main port)
	Port string `yaml:"port,omitempty" json:"port,omitempty"`

	// Interval for Prometheus scraping (default: 30s)
	Interval string `yaml:"interval,omitempty" json:"interval,omitempty"`
}

// TracingConfig configures distributed tracing sidecar injection
type TracingConfig struct {
	// Enabled injects the tracing agent sidecar
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Backend is the tracing backend: "jaeger" or "zipkin" (default: jaeger)
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`

	// SamplingRate is the trace sampling rate from 0.0 to 1.0 (default: 0.1)
	SamplingRate float64 `yaml:"samplingRate,omitempty" json:"samplingRate,omitempty"`

	// CollectorEndpoint is the URL of the trace collector
	CollectorEndpoint string `yaml:"collectorEndpoint,omitempty" json:"collectorEndpoint,omitempty"`

	// AgentImage overrides the default tracing agent image
	AgentImage string `yaml:"agentImage,omitempty" json:"agentImage,omitempty"`
}

// SecurityConfig allows customization of security settings
type SecurityConfig struct {
	// ReadOnlyRootFilesystem sets the container's root filesystem to read-only (default: true)
	ReadOnlyRootFilesystem *bool `yaml:"readOnlyRootFilesystem,omitempty" json:"readOnlyRootFilesystem,omitempty"`
}

// JobConfig defines a Job or CronJob
type JobConfig struct {
	// Name of the job
	Name string `yaml:"name" json:"name"`

	// Image for the job (defaults to app image if not specified)
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Command to run
	Command []string `yaml:"command" json:"command"`

	// Args for the command
	Args []string `yaml:"args,omitempty" json:"args,omitempty"`

	// Schedule in cron format (makes this a CronJob)
	Schedule string `yaml:"schedule,omitempty" json:"schedule,omitempty"`

	// RunBefore specifies when to run (e.g., "deploy" for pre-deploy hooks)
	RunBefore string `yaml:"runBefore,omitempty" json:"runBefore,omitempty"`

	// Env variables for the job
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// BackoffLimit specifies the number of retries before marking as failed
	BackoffLimit *int32 `yaml:"backoffLimit,omitempty" json:"backoffLimit,omitempty"`

	// TTLSecondsAfterFinished limits the lifetime of finished jobs
	TTLSecondsAfterFinished *int32 `yaml:"ttlSecondsAfterFinished,omitempty" json:"ttlSecondsAfterFinished,omitempty"`
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

	// Push configuration for registry push
	Push *PushConfig `yaml:"push,omitempty" json:"push,omitempty"`

	// Tag strategy for image tagging (kbox-timestamp, git-sha, git-tag, latest)
	Tag string `yaml:"tag,omitempty" json:"tag,omitempty"`
}

// PushConfig defines image push configuration
type PushConfig struct {
	// Enabled controls push behavior: auto, always, never (default: auto)
	Enabled string `yaml:"enabled,omitempty" json:"enabled,omitempty"`

	// Registry is the target registry URL (e.g., 123456789.dkr.ecr.us-east-1.amazonaws.com/my-app)
	Registry string `yaml:"registry,omitempty" json:"registry,omitempty"`
}

// Tag strategy constants
const (
	TagKboxTimestamp = "kbox-timestamp"
	TagGitSha        = "git-sha"
	TagGitTag        = "git-tag"
	TagLatest        = "latest"
)

// Push behavior constants
const (
	PushAuto   = "auto"
	PushAlways = "always"
	PushNever  = "never"
)

// SecretsConfig defines secret sources
type SecretsConfig struct {
	// FromEnvFile loads secrets from a .env file (simple, v0.1)
	FromEnvFile string `yaml:"fromEnvFile,omitempty" json:"fromEnvFile,omitempty"`

	// FromSops loads secrets from sops-encrypted files (v0.2+)
	FromSops []string `yaml:"fromSops,omitempty" json:"fromSops,omitempty"`

	// External configures ExternalSecret CRD generation for External Secrets Operator
	External *ExternalSecretConfig `yaml:"external,omitempty" json:"external,omitempty"`

	// FromSecrets references existing Kubernetes secrets
	FromSecrets []SecretRef `yaml:"fromSecrets,omitempty" json:"fromSecrets,omitempty"`
}

// ExternalSecretConfig defines configuration for External Secrets Operator integration
type ExternalSecretConfig struct {
	// StoreRef references the SecretStore or ClusterSecretStore
	StoreRef SecretStoreRef `yaml:"storeRef" json:"storeRef"`

	// RefreshInterval specifies how often to refresh secrets (default: 1h)
	RefreshInterval string `yaml:"refreshInterval,omitempty" json:"refreshInterval,omitempty"`

	// Data specifies individual secret key mappings
	Data []ExternalSecretData `yaml:"data,omitempty" json:"data,omitempty"`

	// DataFrom extracts all keys from a remote secret
	DataFrom []ExternalSecretDataFrom `yaml:"dataFrom,omitempty" json:"dataFrom,omitempty"`
}

// SecretStoreRef references a SecretStore or ClusterSecretStore
type SecretStoreRef struct {
	// Name of the SecretStore/ClusterSecretStore
	Name string `yaml:"name" json:"name"`

	// Kind is either SecretStore or ClusterSecretStore (default: ClusterSecretStore)
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
}

// ExternalSecretData maps a remote secret key to a local env var
type ExternalSecretData struct {
	// EnvVar is the container environment variable name
	EnvVar string `yaml:"envVar" json:"envVar"`

	// RemoteKey is the path/key in the secret provider
	RemoteKey string `yaml:"remoteKey" json:"remoteKey"`

	// Property extracts a specific JSON property from the secret value
	Property string `yaml:"property,omitempty" json:"property,omitempty"`
}

// ExternalSecretDataFrom extracts all keys from a remote secret
type ExternalSecretDataFrom struct {
	// RemoteKey is the path/key in the secret provider to extract all keys from
	RemoteKey string `yaml:"remoteKey" json:"remoteKey"`
}

// SecretRef references an existing Kubernetes secret
type SecretRef struct {
	// Name of the existing secret
	Name string `yaml:"name" json:"name"`

	// Optional marks the secret reference as optional
	Optional bool `yaml:"optional,omitempty" json:"optional,omitempty"`
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

	// IngressClass specifies which ingress controller to use (e.g., "nginx", "traefik")
	IngressClass string `yaml:"ingressClass,omitempty" json:"ingressClass,omitempty"`

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

	// ClusterIssuer for cert-manager automatic certificate provisioning
	ClusterIssuer string `yaml:"clusterIssuer,omitempty" json:"clusterIssuer,omitempty"`
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
	// Namespace to deploy to (for kbox promote)
	Namespace string `yaml:"namespace,omitempty" json:"namespace,omitempty"`

	// Context is the Kubernetes context (for kbox promote to different clusters)
	Context string `yaml:"context,omitempty" json:"context,omitempty"`

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
	APIVersion   string                      `yaml:"apiVersion" json:"apiVersion"`
	Kind         string                      `yaml:"kind" json:"kind"` // "MultiApp"
	Metadata     Metadata                    `yaml:"metadata" json:"metadata"`
	Services     map[string]ServiceSpec      `yaml:"services" json:"services"`
	Environments map[string]MultiEnvOverride `yaml:"environments,omitempty" json:"environments,omitempty"`
}

// MultiEnvOverride defines environment-specific overrides for multi-service apps
type MultiEnvOverride struct {
	// Services contains per-service overrides
	Services map[string]ServiceEnvOverride `yaml:"services,omitempty" json:"services,omitempty"`
}

// ServiceEnvOverride defines environment-specific overrides for a single service
type ServiceEnvOverride struct {
	// Replicas override
	Replicas *int `yaml:"replicas,omitempty" json:"replicas,omitempty"`

	// Env variables to add/override
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// Resources override
	Resources *ResourceConfig `yaml:"resources,omitempty" json:"resources,omitempty"`

	// Image override
	Image string `yaml:"image,omitempty" json:"image,omitempty"`
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

// ForEnvironment returns a config merged with environment-specific overrides.
// Returns an error if the specified environment does not exist in the config.
func (c *AppConfig) ForEnvironment(env string) (*AppConfig, error) {
	if env == "" {
		return c, nil
	}

	if c.Environments == nil {
		return nil, fmt.Errorf("environment %q not found (no environments defined in config)", env)
	}

	override, ok := c.Environments[env]
	if !ok {
		// List available environments for helpful error
		available := make([]string, 0, len(c.Environments))
		for name := range c.Environments {
			available = append(available, name)
		}
		sort.Strings(available)
		return nil, fmt.Errorf("environment %q not found\n  Available: %s", env, strings.Join(available, ", "))
	}

	// Create a copy
	result := *c

	// Deep copy all map[string]string fields to avoid mutating the original
	if c.Metadata.Labels != nil {
		newLabels := make(map[string]string, len(c.Metadata.Labels))
		for k, v := range c.Metadata.Labels {
			newLabels[k] = v
		}
		result.Metadata.Labels = newLabels
	}
	if c.Spec.Env != nil {
		newEnv := make(map[string]string, len(c.Spec.Env))
		for k, v := range c.Spec.Env {
			newEnv[k] = v
		}
		result.Spec.Env = newEnv
	}
	if c.Spec.Labels != nil {
		newLabels := make(map[string]string, len(c.Spec.Labels))
		for k, v := range c.Spec.Labels {
			newLabels[k] = v
		}
		result.Spec.Labels = newLabels
	}
	if c.Spec.Annotations != nil {
		newAnnotations := make(map[string]string, len(c.Spec.Annotations))
		for k, v := range c.Spec.Annotations {
			newAnnotations[k] = v
		}
		result.Spec.Annotations = newAnnotations
	}
	if c.Spec.PodAnnotations != nil {
		newPodAnnotations := make(map[string]string, len(c.Spec.PodAnnotations))
		for k, v := range c.Spec.PodAnnotations {
			newPodAnnotations[k] = v
		}
		result.Spec.PodAnnotations = newPodAnnotations
	}
	if c.Spec.ServiceAnnotations != nil {
		newServiceAnnotations := make(map[string]string, len(c.Spec.ServiceAnnotations))
		for k, v := range c.Spec.ServiceAnnotations {
			newServiceAnnotations[k] = v
		}
		result.Spec.ServiceAnnotations = newServiceAnnotations
	}
	if c.Spec.DeploymentAnnotations != nil {
		newDeploymentAnnotations := make(map[string]string, len(c.Spec.DeploymentAnnotations))
		for k, v := range c.Spec.DeploymentAnnotations {
			newDeploymentAnnotations[k] = v
		}
		result.Spec.DeploymentAnnotations = newDeploymentAnnotations
	}

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

	return &result, nil
}
