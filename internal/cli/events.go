package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
)

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Stream Kubernetes events for your app",
	Long: `Stream and display Kubernetes events for your application.

Shows events related to your app's pods, deployments, and replica sets with
colorized output and filtering options.

Examples:
  kbox events                    # Stream events for app (live)
  kbox events --no-follow        # Show recent events and exit
  kbox events --since 1h         # Events from last hour
  kbox events --since 30m        # Events from last 30 minutes
  kbox events --type Warning     # Only warning events`,
	RunE: runEvents,
}

func init() {
	eventsCmd.Flags().Bool("no-follow", false, "Show recent events and exit (don't stream)")
	eventsCmd.Flags().Duration("since", 0, "Show events from duration ago (e.g., 1h, 30m)")
	eventsCmd.Flags().StringSlice("type", nil, "Filter by event type: Normal, Warning")

	rootCmd.AddCommand(eventsCmd)
}

func runEvents(cmd *cobra.Command, args []string) error {
	noFollow, _ := cmd.Flags().GetBool("no-follow")
	since, _ := cmd.Flags().GetDuration("since")
	types, _ := cmd.Flags().GetStringSlice("type")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

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

	// Print header
	fmt.Fprintf(os.Stderr, "Events for %s (%s namespace)\n\n", appName, ns)
	debug.PrintEventsHeader(os.Stdout)

	// Validate event type filters
	validEventTypes := map[string]bool{"normal": true, "warning": true}
	for _, t := range types {
		if !validEventTypes[strings.ToLower(t)] {
			return fmt.Errorf("invalid event type: %s\n  Valid types: Normal, Warning", t)
		}
	}

	// Build filter
	filter := debug.EventFilter{
		AppName: appName,
		Since:   since,
	}

	// Normalize type filters (manual title case to avoid deprecated strings.Title)
	for _, t := range types {
		normalized := strings.ToLower(t)
		if len(normalized) > 0 {
			normalized = strings.ToUpper(normalized[:1]) + normalized[1:]
		}
		filter.Types = append(filter.Types, normalized)
	}

	// If --since not specified and following, show last 5 minutes by default
	if since == 0 && !noFollow {
		filter.Since = 5 * time.Minute
	}

	// Build options
	opts := debug.EventsOptions{
		Follow: !noFollow,
		Filter: filter,
		Output: os.Stdout,
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr)
		cancel()
	}()

	// Stream events
	return debug.StreamEvents(ctx, client.Clientset, ns, opts)
}
