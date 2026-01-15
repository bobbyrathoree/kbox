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
	"golang.org/x/term"
)

var shellCmd = &cobra.Command{
	Use:   "shell <app>",
	Short: "Open a shell in an app's container",
	Long: `Open an interactive shell in a running container.

Unlike kubectl exec, kbox shell:
  - Auto-detects the main container (skips sidecars)
  - Tries /bin/bash first, falls back to /bin/sh
  - Uses ephemeral debug container for distroless images
  - Selects a pod automatically if multiple exist

Examples:
  kbox shell myapp              # Shell into myapp
  kbox shell myapp -c sidecar   # Shell into specific container
  kbox shell myapp -- ls -la    # Run a command instead of shell`,
	Args: cobra.MinimumNArgs(1),
	RunE: runShell,
}

func runShell(cmd *cobra.Command, args []string) error {
	appName := args[0]

	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")
	container, _ := cmd.Flags().GetString("container")

	// Set up signal handling for graceful cancellation
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

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
	pods, err := debug.FindPods(ctx, client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	// Pick the first ready pod, or just the first one
	var targetPod debug.PodInfo
	for _, p := range pods {
		if p.Ready {
			targetPod = p
			break
		}
	}
	if targetPod.Name == "" {
		targetPod = pods[0]
	}

	// Determine the command to run
	var shellCommand []string
	if len(args) > 1 {
		shellCommand = args[1:]
	}

	// Check if we have a TTY
	isTTY := term.IsTerminal(int(os.Stdin.Fd()))

	fmt.Fprintf(os.Stderr, "Connecting to %s/%s...\n", ns, targetPod.Name)

	opts := debug.ShellOptions{
		Container: container,
		Command:   shellCommand,
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		TTY:       isTTY,
	}

	result, err := debug.Shell(ctx, client.Clientset, client.RestConfig, ns, targetPod.Name, opts)
	if err != nil {
		return err
	}

	if result.UsedEphemeral {
		fmt.Fprintf(os.Stderr, "\nUsed ephemeral container: %s\n", result.EphemeralName)
		fmt.Fprintf(os.Stderr, "Note: The ephemeral container will remain until the pod restarts.\n")
	}

	return nil
}

func init() {
	shellCmd.Flags().StringP("container", "c", "", "Container name (auto-detected if not specified)")
	rootCmd.AddCommand(shellCmd)
}
