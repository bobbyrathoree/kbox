package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/rollout"
)

var rolloutCmd = &cobra.Command{
	Use:   "rollout <subcommand>",
	Short: "Manage deployment rollouts",
	Long: `Manage deployment rollouts for your application.

View rollout status, pause/resume rollouts, and undo deployments.

Subcommands:
  status     Show current rollout status
  pause      Pause an in-progress rollout
  resume     Resume a paused rollout
  undo       Undo current rollout to previous version

Examples:
  kbox rollout status              # Show rollout status
  kbox rollout status --watch      # Watch rollout progress
  kbox rollout pause               # Pause current rollout
  kbox rollout resume              # Resume paused rollout
  kbox rollout undo                # Undo to previous version`,
}

var rolloutStatusCmd = &cobra.Command{
	Use:   "status [app]",
	Short: "Show rollout status",
	Long: `Display the current status of a deployment rollout.

Shows:
  - Deployment state (Progressing, Complete, Failed, Paused)
  - Number of updated/ready pods
  - Pod status breakdown
  - Current and previous images

Use --watch to continuously monitor rollout progress.

Examples:
  kbox rollout status              # Status from kbox.yaml
  kbox rollout status myapp        # Status for specific app
  kbox rollout status --watch      # Watch progress live`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRolloutStatus,
}

var rolloutPauseCmd = &cobra.Command{
	Use:   "pause [app]",
	Short: "Pause rollout",
	Long: `Pause an in-progress deployment rollout.

Pausing stops new pods from being created but doesn't terminate
existing pods. Use 'kbox rollout resume' to continue.

Examples:
  kbox rollout pause               # Pause app from kbox.yaml
  kbox rollout pause myapp         # Pause specific app`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRolloutPause,
}

var rolloutResumeCmd = &cobra.Command{
	Use:   "resume [app]",
	Short: "Resume rollout",
	Long: `Resume a paused deployment rollout.

Examples:
  kbox rollout resume              # Resume app from kbox.yaml
  kbox rollout resume myapp        # Resume specific app`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRolloutResume,
}

var rolloutUndoCmd = &cobra.Command{
	Use:   "undo [app]",
	Short: "Undo rollout",
	Long: `Undo the current rollout to the previous version.

This reverts the deployment's pod template to the previous revision.
For a full configuration rollback, use 'kbox rollback' instead.

Difference from 'kbox rollback':
  - 'undo' reverts the pod template (image, env, etc.) using K8s history
  - 'rollback' restores the full kbox configuration from release history

Examples:
  kbox rollout undo                # Undo app from kbox.yaml
  kbox rollout undo myapp          # Undo specific app`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRolloutUndo,
}

func init() {
	// Status flags
	rolloutStatusCmd.Flags().BoolP("watch", "w", false, "Watch rollout progress")

	// Add subcommands
	rolloutCmd.AddCommand(rolloutStatusCmd)
	rolloutCmd.AddCommand(rolloutPauseCmd)
	rolloutCmd.AddCommand(rolloutResumeCmd)
	rolloutCmd.AddCommand(rolloutUndoCmd)

	rootCmd.AddCommand(rolloutCmd)
}

func runRolloutStatus(cmd *cobra.Command, args []string) error {
	watch, _ := cmd.Flags().GetBool("watch")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Get app name
	appName, err := resolveAppName(args)
	if err != nil {
		return err
	}

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

	if watch {
		return watchRolloutStatus(cmd, client, ns, appName)
	}

	// Get and print status
	status, err := rollout.GetStatus(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	rollout.PrintStatus(os.Stdout, status)
	return nil
}

func watchRolloutStatus(cmd *cobra.Command, client *k8s.Client, namespace, appName string) error {
	fmt.Fprintf(os.Stderr, "Watching %s rollout...\n\n", appName)

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastState := ""

	for {
		select {
		case <-sigCh:
			fmt.Fprintln(os.Stderr, "\nStopped watching (rollout continues in background)")
			return nil
		case <-ticker.C:
			status, err := rollout.GetStatus(cmd.Context(), client.Clientset, namespace, appName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				continue
			}

			// Clear screen and print status
			fmt.Print("\033[H\033[2J") // Clear screen
			fmt.Fprintf(os.Stdout, "Watching %s rollout...\n\n", appName)
			rollout.PrintProgressBar(os.Stdout, status.UpdatedReplicas, status.DesiredReplicas, 30)
			fmt.Fprintln(os.Stdout)
			rollout.PrintStatus(os.Stdout, status)
			fmt.Fprintln(os.Stdout)
			fmt.Fprintln(os.Stdout, "Ctrl+C to stop watching (rollout continues)")

			// Stop if complete or failed
			if status.State == "Complete" && lastState != "Complete" {
				fmt.Fprintln(os.Stdout, "\nRollout complete!")
				return nil
			}
			if status.State == "Failed" && lastState != "Failed" {
				fmt.Fprintln(os.Stderr, "\nRollout failed!")
				return fmt.Errorf("rollout failed: %s", status.Message)
			}
			lastState = status.State
		}
	}
}

func runRolloutPause(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	appName, err := resolveAppName(args)
	if err != nil {
		return err
	}

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

	err = rollout.Pause(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	// Get current status
	status, _ := rollout.GetStatus(cmd.Context(), client.Clientset, ns, appName)

	fmt.Fprintf(os.Stdout, "Paused deployment %s\n", appName)
	if status != nil {
		fmt.Fprintf(os.Stdout, "  Current state: %d/%d pods updated\n", status.UpdatedReplicas, status.DesiredReplicas)
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "  To resume: kbox rollout resume")
	fmt.Fprintln(os.Stdout, "  To undo:   kbox rollout undo")

	return nil
}

func runRolloutResume(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	appName, err := resolveAppName(args)
	if err != nil {
		return err
	}

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

	err = rollout.Resume(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	status, _ := rollout.GetStatus(cmd.Context(), client.Clientset, ns, appName)

	fmt.Fprintf(os.Stdout, "Resumed deployment %s\n", appName)
	if status != nil {
		fmt.Fprintf(os.Stdout, "  Continuing rollout from %d/%d pods...\n", status.UpdatedReplicas, status.DesiredReplicas)
	}

	return nil
}

func runRolloutUndo(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	appName, err := resolveAppName(args)
	if err != nil {
		return err
	}

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

	// Get current and previous images for display
	status, _ := rollout.GetStatus(cmd.Context(), client.Clientset, ns, appName)
	prevImage, _ := rollout.GetPreviousImage(cmd.Context(), client.Clientset, ns, appName)

	fmt.Fprintf(os.Stdout, "Undoing current rollout for %s...\n\n", appName)
	if status != nil && prevImage != "" {
		fmt.Fprintf(os.Stdout, "  Current image: %s\n", status.Image)
		fmt.Fprintf(os.Stdout, "  Reverting to:  %s\n\n", prevImage)
	}

	err = rollout.Undo(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "Undo complete")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Note: This reverts the pod template only.")
	fmt.Fprintln(os.Stdout, "      For full config rollback, use: kbox rollback")

	return nil
}

// resolveAppName gets app name from args or kbox.yaml
func resolveAppName(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	// Try to load from kbox.yaml
	loader := config.NewLoader(".")
	isMulti, err := loader.IsMultiService()
	if err != nil {
		return "", fmt.Errorf("no app specified and no kbox.yaml found\n  -> Run 'kbox rollout status <app>' or create kbox.yaml")
	}

	if isMulti {
		cfg, err := loader.LoadMultiService()
		if err != nil {
			return "", fmt.Errorf("failed to load kbox.yaml: %w", err)
		}
		// Use first service
		for name := range cfg.Services {
			return name, nil
		}
		return "", fmt.Errorf("no services defined in kbox.yaml")
	}

	cfg, err := loader.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	return cfg.Metadata.Name, nil
}
