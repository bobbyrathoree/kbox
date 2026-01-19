package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/exec"
	"github.com/bobbyrathoree/kbox/internal/k8s"
)

var execCmd = &cobra.Command{
	Use:   "exec <command>",
	Short: "Run one-off command in fresh pod",
	Long: `Run a one-off command in a fresh temporary pod.

Creates a new pod using the app's image, runs the command, streams output,
and cleans up the pod when done.

Different from 'kbox shell' which opens a shell in an existing running pod.
Use 'kbox exec' for migrations, one-time scripts, or debugging with a clean state.

Examples:
  kbox exec "npm run migrate"           # Run database migration
  kbox exec "python manage.py shell"    # Django shell
  kbox exec -it bash                    # Interactive bash
  kbox exec --image node:18 "npm test"  # Use different image
  kbox exec "rake db:seed"              # Rails seed data`,
	Args: cobra.ExactArgs(1),
	RunE: runExec,
}

func init() {
	execCmd.Flags().BoolP("interactive", "i", false, "Keep stdin open")
	execCmd.Flags().BoolP("tty", "t", false, "Allocate a TTY")
	execCmd.Flags().String("image", "", "Override image (default: from kbox.yaml)")
	execCmd.Flags().Duration("timeout", 10*time.Minute, "Timeout for command execution")

	rootCmd.AddCommand(execCmd)
}

func runExec(cmd *cobra.Command, args []string) error {
	interactive, _ := cmd.Flags().GetBool("interactive")
	tty, _ := cmd.Flags().GetBool("tty")
	imageOverride, _ := cmd.Flags().GetString("image")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	command := args[0]

	// Auto-detect TTY if interactive
	if interactive && term.IsTerminal(int(os.Stdin.Fd())) {
		tty = true
	}

	// Load config to get image and app name
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %v\n  Run 'kbox init' to create one", err)
	}

	// Determine image to use
	image := imageOverride
	if image == "" {
		image = cfg.Spec.Image
		if image == "" {
			return fmt.Errorf("no image specified\n  Either set spec.image in kbox.yaml or use --image flag")
		}
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

	// Build exec options
	opts := exec.Options{
		Image:       image,
		Command:     command,
		Namespace:   ns,
		AppName:     cfg.Metadata.Name,
		Interactive: interactive,
		TTY:         tty,
		Timeout:     timeout,
		EnvVars:     cfg.Spec.Env,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}

	// Create runner
	runner := exec.NewRunner(client.Clientset, client.RestConfig)

	// Set up signal handling for graceful cleanup
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nInterrupted, cleaning up...")
		cancel()
	}()

	// Run the command
	result, err := runner.Run(ctx, opts)
	if err != nil {
		return err
	}

	// Exit with the command's exit code
	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}

	return nil
}
