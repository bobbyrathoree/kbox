package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Collect CPU or memory profile from running pod",
	Long: `Collect CPU or memory profiles from a running pod via HTTP pprof.

Connects to your app's pprof endpoint, collects the profile data, and saves
it to a local file for analysis with 'go tool pprof'.

Your Go application must have pprof enabled:
  import _ "net/http/pprof"
  go http.ListenAndServe(":6060", nil)

Examples:
  kbox profile                      # 30s CPU profile
  kbox profile --duration 60s       # 60 second CPU profile
  kbox profile --type heap          # Memory/heap profile
  kbox profile --type goroutine     # Goroutine stacks
  kbox profile --output cpu.pb.gz   # Save to specific file
  kbox profile --port 8080          # Custom pprof port`,
	RunE: runProfile,
}

func init() {
	profileCmd.Flags().DurationP("duration", "d", 30*time.Second, "Profile duration (for CPU profiles)")
	profileCmd.Flags().StringP("type", "t", "cpu", "Profile type: cpu, heap, goroutine, block, mutex, allocs")
	profileCmd.Flags().StringP("output", "o", "", "Output file path (default: auto-generated)")
	profileCmd.Flags().IntP("port", "p", 6060, "pprof HTTP port on the pod")

	rootCmd.AddCommand(profileCmd)
}

func runProfile(cmd *cobra.Command, args []string) error {
	duration, _ := cmd.Flags().GetDuration("duration")
	profileType, _ := cmd.Flags().GetString("type")
	outputPath, _ := cmd.Flags().GetString("output")
	pprofPort, _ := cmd.Flags().GetInt("port")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Validate port range
	if pprofPort < 1 || pprofPort > 65535 {
		return fmt.Errorf("invalid port: %d\n  Port must be between 1 and 65535", pprofPort)
	}

	// Validate duration
	if duration <= 0 {
		return fmt.Errorf("invalid duration: %s\n  Duration must be positive", duration)
	}

	// Validate profile type
	validTypes := map[string]debug.ProfileType{
		"cpu":       debug.ProfileCPU,
		"heap":      debug.ProfileHeap,
		"goroutine": debug.ProfileGoroutine,
		"block":     debug.ProfileBlock,
		"mutex":     debug.ProfileMutex,
		"allocs":    debug.ProfileAllocs,
	}

	pType, ok := validTypes[profileType]
	if !ok {
		return fmt.Errorf("invalid profile type: %s\n  Valid types: cpu, heap, goroutine, block, mutex, allocs", profileType)
	}

	// Load config to get app name
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %v\n  Run 'kbox init' to create one", err)
	}

	appName := cfg.Metadata.Name

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

	// Find a ready pod
	pods, err := debug.FindPods(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return fmt.Errorf("failed to find pods: %w", err)
	}

	// Select a ready pod
	var targetPod debug.PodInfo
	for _, pod := range pods {
		if pod.Ready && pod.Status == "Running" {
			targetPod = pod
			break
		}
	}

	if targetPod.Name == "" {
		// Fall back to first pod if none are ready
		if len(pods) > 0 {
			targetPod = pods[0]
		} else {
			return fmt.Errorf("no pods found for %s", appName)
		}
	}

	// Print header
	fmt.Fprintf(os.Stderr, "Profiling %s...\n", appName)
	fmt.Fprintf(os.Stderr, "  Pod:  %s\n", targetPod.Name)
	fmt.Fprintf(os.Stderr, "  Port: %d (pprof)\n", pprofPort)
	fmt.Fprintf(os.Stderr, "  Type: %s\n", profileType)
	if pType == debug.ProfileCPU {
		fmt.Fprintf(os.Stderr, "  Duration: %s\n", duration)
	}
	fmt.Fprintln(os.Stderr)

	// Set up signal handling
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nInterrupted")
		cancel()
	}()

	// Build options
	opts := debug.ProfileOptions{
		Type:       pType,
		Duration:   duration,
		Port:       pprofPort,
		OutputPath: outputPath,
		AppName:    appName,
		Output:     os.Stderr,
	}

	// Collect profile
	result, err := debug.CollectProfile(ctx, client.Clientset, client.RestConfig, ns, targetPod, opts)
	if err != nil {
		return err
	}

	// Print result
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Profile collected: %s\n", debug.FormatProfileSize(result.Size))
	fmt.Fprintf(os.Stderr, "  Saved to: %s\n", result.FilePath)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "View with:")
	fmt.Fprintf(os.Stderr, "  go tool pprof %s\n", result.FilePath)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Or open in browser:")
	fmt.Fprintf(os.Stderr, "  go tool pprof -http=:8080 %s\n", result.FilePath)

	return nil
}
