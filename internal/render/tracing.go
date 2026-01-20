package render

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/bobbyrathoree/kbox/internal/config"
)

const (
	// Default images for tracing agents
	DefaultJaegerAgentImage = "jaegertracing/jaeger-agent:1.53"
	DefaultZipkinAgentImage = "openzipkin/zipkin-slim:2.24"

	// Default ports
	JaegerThriftCompactPort = 6831
	JaegerThriftBinaryPort  = 6832
	ZipkinPort              = 9411
)

// sidecarSecurityContext returns a hardened SecurityContext for sidecar containers
func sidecarSecurityContext() *corev1.SecurityContext {
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := true

	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// RenderTracingSidecar creates a tracing agent sidecar container
func RenderTracingSidecar(cfg *config.TracingConfig) corev1.Container {
	backend := cfg.Backend
	if backend == "" {
		backend = "jaeger"
	}

	switch backend {
	case "zipkin":
		return renderZipkinSidecar(cfg)
	default:
		return renderJaegerSidecar(cfg)
	}
}

// renderJaegerSidecar creates a Jaeger agent sidecar
func renderJaegerSidecar(cfg *config.TracingConfig) corev1.Container {
	image := cfg.AgentImage
	if image == "" {
		image = DefaultJaegerAgentImage
	}

	// Build args
	args := []string{}
	if cfg.CollectorEndpoint != "" {
		args = append(args, "--reporter.grpc.host-port="+cfg.CollectorEndpoint)
	}

	container := corev1.Container{
		Name:  "jaeger-agent",
		Image: image,
		Args:  args,
		Ports: []corev1.ContainerPort{
			{
				Name:          "thrift-compact",
				ContainerPort: JaegerThriftCompactPort,
				Protocol:      corev1.ProtocolUDP,
			},
			{
				Name:          "thrift-binary",
				ContainerPort: JaegerThriftBinaryPort,
				Protocol:      corev1.ProtocolUDP,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
		},
		SecurityContext: sidecarSecurityContext(),
	}

	return container
}

// renderZipkinSidecar creates a Zipkin agent sidecar
func renderZipkinSidecar(cfg *config.TracingConfig) corev1.Container {
	image := cfg.AgentImage
	if image == "" {
		image = DefaultZipkinAgentImage
	}

	container := corev1.Container{
		Name:  "zipkin-agent",
		Image: image,
		Ports: []corev1.ContainerPort{
			{
				Name:          "zipkin",
				ContainerPort: ZipkinPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
		SecurityContext: sidecarSecurityContext(),
	}

	return container
}

// GetTracingEnvVars returns environment variables to inject into the app container
// These configure the application's tracing client to send to the local sidecar
func GetTracingEnvVars(cfg *config.TracingConfig) []corev1.EnvVar {
	backend := cfg.Backend
	if backend == "" {
		backend = "jaeger"
	}

	samplingRate := cfg.SamplingRate
	if samplingRate <= 0 {
		samplingRate = 0.1 // 10% default
	}
	if samplingRate > 1 {
		samplingRate = 1.0
	}

	var envVars []corev1.EnvVar

	switch backend {
	case "zipkin":
		envVars = []corev1.EnvVar{
			{Name: "ZIPKIN_ENDPOINT", Value: fmt.Sprintf("http://localhost:%d/api/v2/spans", ZipkinPort)},
			{Name: "ZIPKIN_SAMPLE_RATE", Value: strconv.FormatFloat(samplingRate, 'f', 2, 64)},
		}
	default: // jaeger
		envVars = []corev1.EnvVar{
			{Name: "JAEGER_AGENT_HOST", Value: "localhost"},
			{Name: "JAEGER_AGENT_PORT", Value: strconv.Itoa(JaegerThriftCompactPort)},
			{Name: "JAEGER_SAMPLER_TYPE", Value: "probabilistic"},
			{Name: "JAEGER_SAMPLER_PARAM", Value: strconv.FormatFloat(samplingRate, 'f', 2, 64)},
			// OpenTelemetry compatibility
			{Name: "OTEL_EXPORTER_JAEGER_AGENT_HOST", Value: "localhost"},
			{Name: "OTEL_EXPORTER_JAEGER_AGENT_PORT", Value: strconv.Itoa(JaegerThriftCompactPort)},
			{Name: "OTEL_TRACES_SAMPLER", Value: "parentbased_traceidratio"},
			{Name: "OTEL_TRACES_SAMPLER_ARG", Value: strconv.FormatFloat(samplingRate, 'f', 2, 64)},
		}
	}

	return envVars
}
