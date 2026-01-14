package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <app>",
	Short: "Stream logs from an app with K8s events interleaved",
	Long: `Stream logs from all pods of an application.

Unlike kubectl logs, kbox logs:
  - Streams from ALL pods automatically
  - Interleaves K8s events (OOMKilled, probe failures, restarts)
  - Auto-fetches previous container logs if restarting
  - Color-codes output for easy reading

Examples:
  kbox logs myapp              # Stream logs from all myapp pods
  kbox logs myapp --no-follow  # Print recent logs and exit
  kbox logs myapp --previous   # Show previous container logs
  kbox logs myapp --no-events  # Disable event interleaving`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

func runLogs(cmd *cobra.Command, args []string) error {
	appName := args[0]

	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")
	follow, _ := cmd.Flags().GetBool("follow")
	timestamps, _ := cmd.Flags().GetBool("timestamps")
	tailLines, _ := cmd.Flags().GetInt64("tail")
	previous, _ := cmd.Flags().GetBool("previous")
	showEvents, _ := cmd.Flags().GetBool("events")

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	ns := client.Namespace
	if namespace != "" {
		ns = namespace
	}

	// Find pods for the app
	pods, err := debug.FindPods(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	// Print header
	if len(pods) == 1 {
		fmt.Fprintf(os.Stderr, "Streaming logs from %s/%s\n", ns, pods[0].Name)
	} else {
		fmt.Fprintf(os.Stderr, "Streaming logs from %d pods in %s\n", len(pods), ns)
		for _, p := range pods {
			status := "ready"
			if !p.Ready {
				status = fmt.Sprintf("not ready, %d restarts", p.Restarts)
			}
			fmt.Fprintf(os.Stderr, "  - %s (%s)\n", p.Name, status)
		}
	}
	if showEvents {
		fmt.Fprintf(os.Stderr, "K8s events will be shown inline (yellow)\n")
	}
	fmt.Fprintln(os.Stderr)

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nStopping log stream...")
		cancel()
	}()

	opts := debug.LogsOptions{
		Follow:       follow,
		Timestamps:   timestamps,
		TailLines:    tailLines,
		Previous:     previous,
		AutoPrevious: true,
		ShowEvents:   showEvents,
	}

	return debug.StreamLogs(ctx, client.Clientset, ns, pods, opts, os.Stdout)
}

func init() {
	logsCmd.Flags().BoolP("follow", "f", true, "Follow log output")
	logsCmd.Flags().BoolP("timestamps", "t", true, "Show timestamps")
	logsCmd.Flags().Int64("tail", 100, "Number of lines to show from the end")
	logsCmd.Flags().BoolP("previous", "p", false, "Show previous container logs")
	logsCmd.Flags().Bool("events", true, "Show K8s events interleaved with logs")
	logsCmd.Flags().Bool("no-follow", false, "Don't follow, just print recent logs")
	logsCmd.Flags().Bool("no-events", false, "Don't show K8s events")

	rootCmd.AddCommand(logsCmd)
}
