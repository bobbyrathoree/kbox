package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/apply"
	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/release"
	"github.com/bobbyrathoree/kbox/internal/render"
)

func newDevCmd() *cobra.Command {
	var (
		watch       bool
		skipLogs    bool
		namespace   string
		kubeContext string
	)

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Development loop: build, deploy, and stream logs",
		Long: `Start a development loop that builds and deploys your app.

By default, uses manual trigger mode: press Enter to rebuild and redeploy.
Use --watch to automatically rebuild when files change.

The dev loop will:
1. Build your Docker image
2. Load it into your local cluster (kind/minikube) or push to registry
3. Update the deployment
4. Stream logs from your app

Press Ctrl+C to exit.`,
		Example: `  # Manual trigger mode (press Enter to rebuild)
  kbox dev

  # Watch mode (auto-rebuild on file changes)
  kbox dev --watch

  # Skip log streaming
  kbox dev --no-logs`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDev(cmd.Context(), devOptions{
				watch:       watch,
				skipLogs:    skipLogs,
				namespace:   namespace,
				kubeContext: kubeContext,
			})
		},
	}

	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch for file changes and auto-rebuild")
	cmd.Flags().BoolVar(&skipLogs, "no-logs", false, "Don't stream logs after deploy")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kubernetes context")

	return cmd
}

type devOptions struct {
	watch       bool
	skipLogs    bool
	namespace   string
	kubeContext string
}

func runDev(ctx context.Context, opts devOptions) error {
	// Load config
	loader := config.NewLoader(".")
	var cfg *config.AppConfig
	var err error

	if loader.HasConfigFile() {
		cfg, err = loader.Load()
		if err != nil {
			return fmt.Errorf("failed to load kbox.yaml: %w", err)
		}
	} else {
		// Zero-config mode - infer from Dockerfile
		cfg, err = config.InferFromDockerfile(".")
		if err != nil {
			return fmt.Errorf("no kbox.yaml found and could not infer from Dockerfile: %w", err)
		}
		fmt.Printf("No kbox.yaml found. Using defaults:\n")
		fmt.Printf("  name: %s (from directory name)\n", cfg.Metadata.Name)
		fmt.Printf("  port: %d (from Dockerfile EXPOSE)\n", cfg.Spec.Port)
		fmt.Println()
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   opts.kubeContext,
		Namespace: opts.namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	targetNS := cfg.Metadata.Namespace
	if targetNS == "" {
		targetNS = client.Namespace
	}
	if opts.namespace != "" {
		targetNS = opts.namespace
	}

	fmt.Printf("kbox dev ready for %s (context: %s, namespace: %s)\n", cfg.Metadata.Name, client.Context, targetNS)
	fmt.Println()

	if opts.watch {
		fmt.Println("Watch mode enabled - will rebuild on file changes")
		fmt.Println("(Note: watch mode is experimental, prefer manual trigger for reliability)")
	} else {
		fmt.Println("Press Enter to build & deploy (Ctrl+C to exit)")
	}
	fmt.Println()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create cancellable context for log streaming
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	// Input channel for manual trigger
	inputChan := make(chan struct{})
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			_, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			select {
			case inputChan <- struct{}{}:
			default:
			}
		}
	}()

	// Dev loop
	iteration := 0
	for {
		select {
		case <-sigChan:
			fmt.Println("\nShutting down...")
			return nil

		case <-inputChan:
			// Manual trigger - rebuild
			cancelStream()
			streamCtx, cancelStream = context.WithCancel(ctx)
			iteration++

			if err := devBuildAndDeploy(streamCtx, cfg, client, targetNS, iteration, opts.skipLogs); err != nil {
				fmt.Printf("\n✗ Error: %v\n\n", err)
				fmt.Println("Press Enter to retry, Ctrl+C to exit")
				continue
			}

			if !opts.skipLogs {
				// Start log streaming in background
				go streamDevLogs(streamCtx, client, targetNS, cfg.Metadata.Name)
			}

			fmt.Println()
			fmt.Println("Press Enter to rebuild, Ctrl+C to exit")
		}
	}
}

func devBuildAndDeploy(ctx context.Context, cfg *config.AppConfig, client *k8s.Client, namespace string, iteration int, skipLogs bool) error {
	startTime := time.Now()

	// Generate image tag
	imageTag := fmt.Sprintf("dev-%d-%d", time.Now().Unix(), iteration)
	imageName := fmt.Sprintf("%s:%s", cfg.Metadata.Name, imageTag)

	fmt.Printf("\n[%d] Building %s...\n", iteration, imageName)

	// Build image
	buildCtx := "."
	dockerfile := "Dockerfile"
	if cfg.Spec.Build != nil {
		if cfg.Spec.Build.Context != "" {
			buildCtx = cfg.Spec.Build.Context
		}
		if cfg.Spec.Build.Dockerfile != "" {
			dockerfile = cfg.Spec.Build.Dockerfile
		}
	}

	buildCmd := exec.CommandContext(ctx, "docker", "build",
		"-t", imageName,
		"-f", dockerfile,
		buildCtx,
	)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	fmt.Printf("Built %s in %v\n", imageName, time.Since(startTime).Round(time.Millisecond))

	// Load image into cluster (uses existing loadImage from up.go)
	if err := loadImage(ctx, client.Context, imageName); err != nil {
		fmt.Printf("Warning: failed to load image into cluster: %v\n", err)
		fmt.Println("If using a remote cluster, ensure the image is pushed to a registry.")
	}

	// Update config with new image
	cfg.Spec.Image = imageName

	// Render and deploy
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return fmt.Errorf("render failed: %w", err)
	}

	fmt.Println("Deploying...")
	engine := apply.NewEngine(client.Clientset, os.Stdout)
	_, err = engine.Apply(ctx, bundle)
	if err != nil {
		return fmt.Errorf("deploy failed: %w", err)
	}

	// Wait for rollout
	if err := engine.WaitForRollout(ctx, namespace, cfg.Metadata.Name); err != nil {
		return fmt.Errorf("rollout failed: %w", err)
	}

	// Save release
	store := release.NewStore(client.Clientset, namespace, cfg.Metadata.Name)
	rev, err := store.Save(ctx, cfg)
	if err != nil {
		fmt.Printf("Warning: failed to save release: %v\n", err)
	} else {
		fmt.Printf("Release %s saved\n", release.FormatRevision(rev))
	}

	totalTime := time.Since(startTime).Round(time.Millisecond)
	fmt.Printf("\n✓ Deploy complete in %v\n", totalTime)

	return nil
}

func streamDevLogs(ctx context.Context, client *k8s.Client, namespace, appName string) {
	// Wait a moment for pods to be ready
	time.Sleep(2 * time.Second)

	// Get pods for the app
	pods, err := debug.FindPods(ctx, client.Clientset, namespace, appName)
	if err != nil || len(pods) == 0 {
		fmt.Printf("\nNo pods found for %s, skipping log streaming\n", appName)
		return
	}

	fmt.Println("\nStreaming logs (Ctrl+C to stop)...")
	fmt.Println()

	opts := debug.DefaultLogsOptions()
	opts.TailLines = 50

	// Stream logs - this blocks until context is cancelled
	debug.StreamLogs(ctx, client.Clientset, namespace, pods, opts, os.Stdout)
}

func init() {
	rootCmd.AddCommand(newDevCmd())
}
