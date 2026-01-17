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

View rollout status, pause/resume rollouts, undo deployments, and run canary releases.

Subcommands:
  status     Show current rollout status
  pause      Pause an in-progress rollout
  resume     Resume a paused rollout
  undo       Undo current rollout to previous version
  canary     Start a canary deployment
  promote    Promote canary to full deployment

Examples:
  kbox rollout status              # Show rollout status
  kbox rollout status --watch      # Watch rollout progress
  kbox rollout pause               # Pause current rollout
  kbox rollout resume              # Resume paused rollout
  kbox rollout undo                # Undo to previous version
  kbox rollout canary --weight 20  # Deploy canary (20% traffic)
  kbox rollout promote             # Promote canary to 100%`,
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

var rolloutCanaryCmd = &cobra.Command{
	Use:   "canary [app]",
	Short: "Start canary deployment",
	Long: `Start a canary deployment to test a new version with a portion of traffic.

Creates a separate canary deployment alongside the main deployment.
Traffic is split based on pod ratio (e.g., 4 main + 1 canary = ~20% canary).

The canary uses the same Service selector, so traffic load-balances between
both deployments automatically.

Flags:
  --weight    Percentage of traffic for canary (1-100, default: 20)
  --image     Image for canary (default: same as main deployment)

Examples:
  kbox rollout canary              # 20% canary with current image
  kbox rollout canary --weight 10  # 10% canary
  kbox rollout canary --image myapp:v2.0.0 --weight 25`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRolloutCanary,
}

var rolloutPromoteCmd = &cobra.Command{
	Use:   "promote [app]",
	Short: "Promote canary deployment",
	Long: `Promote the canary deployment to become the main deployment.

This updates the main deployment's image to match the canary, then
deletes the canary deployment. All traffic will go to the new version.

If no canary exists, this command will fail.

Examples:
  kbox rollout promote             # Promote canary from kbox.yaml
  kbox rollout promote myapp       # Promote specific app's canary`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRolloutPromote,
}

func init() {
	// Status flags
	rolloutStatusCmd.Flags().BoolP("watch", "w", false, "Watch rollout progress")

	// Canary flags
	rolloutCanaryCmd.Flags().IntP("weight", "w", 20, "Traffic percentage for canary (1-100)")
	rolloutCanaryCmd.Flags().StringP("image", "i", "", "Image for canary (default: same as main)")

	// Add subcommands
	rolloutCmd.AddCommand(rolloutStatusCmd)
	rolloutCmd.AddCommand(rolloutPauseCmd)
	rolloutCmd.AddCommand(rolloutResumeCmd)
	rolloutCmd.AddCommand(rolloutUndoCmd)
	rolloutCmd.AddCommand(rolloutCanaryCmd)
	rolloutCmd.AddCommand(rolloutPromoteCmd)

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

	// Check if there's an active canary - if so, abort it
	canaryStatus, _ := rollout.GetCanaryStatus(cmd.Context(), client.Clientset, ns, appName)
	if canaryStatus != nil && canaryStatus.Active {
		fmt.Fprintf(os.Stderr, "Aborting canary deployment for %s...\n\n", appName)
		fmt.Fprintf(os.Stdout, "  Canary: %s (%d pods)\n", canaryStatus.CanaryImage, canaryStatus.CanaryReplicas)
		fmt.Fprintf(os.Stdout, "  Main:   %s (%d pods)\n\n", canaryStatus.MainImage, canaryStatus.MainReplicas)

		err = rollout.AbortCanary(cmd.Context(), client.Clientset, ns, appName)
		if err != nil {
			return err
		}

		fmt.Fprintln(os.Stdout, "Canary aborted successfully!")
		fmt.Fprintf(os.Stdout, "  All traffic now goes to: %s\n", canaryStatus.MainImage)
		return nil
	}

	// No canary, do normal rollout undo
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

func runRolloutCanary(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")
	weight, _ := cmd.Flags().GetInt("weight")
	image, _ := cmd.Flags().GetString("image")

	// Validate weight
	if weight < 1 || weight > 100 {
		return fmt.Errorf("weight must be between 1 and 100, got %d", weight)
	}

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

	fmt.Fprintf(os.Stderr, "Starting canary deployment for %s...\n\n", appName)

	cfg := rollout.CanaryConfig{
		Weight: weight,
		Image:  image,
	}

	status, err := rollout.StartCanary(cmd.Context(), client.Clientset, ns, appName, cfg)
	if err != nil {
		return err
	}

	printCanaryStatus(status)

	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Monitor:  kbox rollout status")
	fmt.Fprintln(os.Stdout, "Promote:  kbox rollout promote")
	fmt.Fprintln(os.Stdout, "Abort:    kbox rollout undo")

	return nil
}

func runRolloutPromote(cmd *cobra.Command, args []string) error {
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

	// Get canary status first
	status, err := rollout.GetCanaryStatus(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}
	if !status.Active {
		return fmt.Errorf("no canary deployment found for %q\n  Start one with: kbox rollout canary", appName)
	}

	fmt.Fprintf(os.Stderr, "Promoting canary for %s...\n\n", appName)
	fmt.Fprintf(os.Stdout, "  Canary image: %s\n", status.CanaryImage)
	fmt.Fprintf(os.Stdout, "  Main image:   %s\n\n", status.MainImage)

	err = rollout.PromoteCanary(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "Canary promoted successfully!")
	fmt.Fprintf(os.Stdout, "  All traffic now goes to: %s\n", status.CanaryImage)

	return nil
}

func printCanaryStatus(status *rollout.CanaryStatus) {
	fmt.Fprintf(os.Stdout, "Canary deployment created:\n")
	fmt.Fprintf(os.Stdout, "  Main:   %s (%d%% traffic, %d pods)\n",
		status.MainImage, 100-status.Weight, status.MainReplicas)
	fmt.Fprintf(os.Stdout, "  Canary: %s (%d%% traffic, %d pods)\n",
		status.CanaryImage, status.Weight, status.CanaryReplicas)
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
