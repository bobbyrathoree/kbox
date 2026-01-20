package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
)

var traceCmd = &cobra.Command{
	Use:   "trace",
	Short: "Manage distributed tracing",
	Long: `View and manage distributed tracing configuration.

Tracing is configured in kbox.yaml and automatically injects a tracing
sidecar (Jaeger or Zipkin agent) into your deployment.

Configuration example in kbox.yaml:
  tracing:
    enabled: true
    backend: jaeger              # or "zipkin"
    samplingRate: 0.1            # 10% of requests
    collectorEndpoint: "jaeger-collector:14250"

Commands:
  status    Show current tracing configuration
  logs      View tracing sidecar logs`,
}

var traceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show tracing configuration status",
	RunE:  runTraceStatus,
}

var traceLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View tracing sidecar logs",
	RunE:  runTraceLogs,
}

func init() {
	traceCmd.AddCommand(traceStatusCmd)
	traceCmd.AddCommand(traceLogsCmd)

	traceLogsCmd.Flags().BoolP("follow", "f", true, "Follow log output")
	traceLogsCmd.Flags().Int64("tail", 100, "Number of lines to show")

	rootCmd.AddCommand(traceCmd)
}

func runTraceStatus(cmd *cobra.Command, args []string) error {
	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %v\n  Run 'kbox init' to create one", err)
	}

	appName := cfg.Metadata.Name

	fmt.Printf("Tracing status for %s:\n\n", appName)

	if cfg.Spec.Tracing == nil || !cfg.Spec.Tracing.Enabled {
		fmt.Println("  Status: \033[33mDisabled\033[0m")
		fmt.Println()
		fmt.Println("To enable tracing, add to kbox.yaml:")
		fmt.Println()
		fmt.Println("  tracing:")
		fmt.Println("    enabled: true")
		fmt.Println("    backend: jaeger")
		fmt.Println("    samplingRate: 0.1")
		fmt.Println("    collectorEndpoint: \"jaeger-collector:14250\"")
		return nil
	}

	tracing := cfg.Spec.Tracing

	// Status
	fmt.Println("  Status: \033[32mEnabled\033[0m")

	// Backend
	backend := tracing.Backend
	if backend == "" {
		backend = "jaeger"
	} else if backend != "jaeger" && backend != "zipkin" {
		fmt.Fprintf(os.Stderr, "Warning: unknown backend %q, defaulting to jaeger\n", backend)
		backend = "jaeger"
	}
	fmt.Printf("  Backend: %s\n", backend)

	// Sampling rate
	samplingRate := tracing.SamplingRate
	if samplingRate <= 0 {
		samplingRate = 0.1
	}
	if samplingRate > 1 {
		samplingRate = 1.0
	}
	fmt.Printf("  Sampling Rate: %.0f%%\n", samplingRate*100)

	// Collector endpoint
	if tracing.CollectorEndpoint != "" {
		fmt.Printf("  Collector: %s\n", tracing.CollectorEndpoint)
	} else {
		fmt.Printf("  Collector: \033[90m(not configured)\033[0m\n")
	}

	// Agent image
	if tracing.AgentImage != "" {
		fmt.Printf("  Agent Image: %s\n", tracing.AgentImage)
	}

	fmt.Println()
	fmt.Println("Environment variables injected into app:")
	if backend == "zipkin" {
		fmt.Println("  ZIPKIN_ENDPOINT")
		fmt.Println("  ZIPKIN_SAMPLE_RATE")
	} else {
		fmt.Println("  JAEGER_AGENT_HOST")
		fmt.Println("  JAEGER_AGENT_PORT")
		fmt.Println("  JAEGER_SAMPLER_TYPE")
		fmt.Println("  JAEGER_SAMPLER_PARAM")
		fmt.Println("  OTEL_EXPORTER_JAEGER_AGENT_HOST")
		fmt.Println("  OTEL_EXPORTER_JAEGER_AGENT_PORT")
	}

	fmt.Println()
	fmt.Println("View sidecar logs:")
	fmt.Println("  kbox trace logs")

	return nil
}

func runTraceLogs(cmd *cobra.Command, args []string) error {
	follow, _ := cmd.Flags().GetBool("follow")
	tailLines, _ := cmd.Flags().GetInt64("tail")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %v\n  Run 'kbox init' to create one", err)
	}

	if cfg.Spec.Tracing == nil || !cfg.Spec.Tracing.Enabled {
		return fmt.Errorf("tracing is not enabled\n  Add 'tracing.enabled: true' to kbox.yaml")
	}

	appName := cfg.Metadata.Name

	// Determine container name based on backend
	backend := cfg.Spec.Tracing.Backend
	if backend == "" {
		backend = "jaeger"
	}
	containerName := "jaeger-agent"
	if backend == "zipkin" {
		containerName = "zipkin-agent"
	}

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Determine namespace
	ns := cfg.Metadata.Namespace
	if namespace != "" {
		ns = namespace
	}
	if ns == "" {
		ns = client.Namespace
	}

	// Find pods
	pods, err := debug.FindPods(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return fmt.Errorf("failed to find pods: %w", err)
	}

	if len(pods) == 0 {
		return fmt.Errorf("no pods found for %s", appName)
	}

	// Override container name in pods to target the sidecar
	for i := range pods {
		pods[i].ContainerName = containerName
	}

	fmt.Fprintf(os.Stderr, "Streaming %s logs from %d pod(s)...\n\n", containerName, len(pods))

	// Set up signal handling
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr)
		cancel()
	}()

	// Stream logs
	opts := debug.LogsOptions{
		Follow:     follow,
		Timestamps: true,
		TailLines:  tailLines,
		ShowEvents: false, // Don't interleave events for sidecar logs
	}

	return debug.StreamLogs(ctx, client.Clientset, ns, pods, opts, os.Stdout)
}
