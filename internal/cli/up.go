package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Build and deploy with zero config",
	Long: `Build and deploy an application with minimal configuration.

This is the "Bun moment" for kbox - it works with just a Dockerfile:
  1. Detects settings from Dockerfile (EXPOSE port, etc.)
  2. Builds a container image
  3. Loads it into your local cluster (kind/minikube)
  4. Deploys and streams logs

If kbox.yaml exists, it will use that for additional configuration.

Examples:
  kbox up              # Build and deploy current directory
  kbox up -e dev       # With environment overlay
  kbox up --no-logs    # Deploy without streaming logs`,
	RunE: runUp,
}

func runUp(cmd *cobra.Command, args []string) error {
	env, _ := cmd.Flags().GetString("env")
	noLogs, _ := cmd.Flags().GetBool("no-logs")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Get working directory
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	appName := filepath.Base(workDir)

	// Try to load config, or infer from Dockerfile
	loader := config.NewLoader(workDir)
	cfg, err := loader.Load()
	if err != nil {
		// Infer from Dockerfile
		cfg, err = config.InferFromDockerfile(workDir)
		if err != nil {
			return fmt.Errorf("no kbox.yaml and no Dockerfile found")
		}
		fmt.Println("No kbox.yaml found. Using defaults:")
		fmt.Printf("  name: %s (from directory)\n", cfg.Metadata.Name)
		fmt.Printf("  port: %d (from Dockerfile EXPOSE)\n", cfg.Spec.Port)
		fmt.Printf("  replicas: %d\n", cfg.Spec.Replicas)
		fmt.Println()
	}

	// Apply environment overlay
	if env != "" {
		cfg = cfg.ForEnvironment(env)
		fmt.Printf("Using environment: %s\n", env)
	}

	// Override namespace if specified
	if namespace != "" {
		cfg.Metadata.Namespace = namespace
	}

	// Connect to cluster first to detect type
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	targetNS := cfg.Metadata.Namespace
	if targetNS == "" {
		targetNS = client.Namespace
	}

	// Generate image tag
	imageTag := fmt.Sprintf("%s:kbox-%d", appName, time.Now().Unix())

	// Build image
	fmt.Printf("Building image: %s\n", imageTag)
	if err := buildImage(cmd.Context(), workDir, imageTag); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	fmt.Println("  ✓ Image built")

	// Load into cluster (detect kind/minikube)
	if err := loadImage(cmd.Context(), client.Context, imageTag); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load image into cluster: %v\n", err)
		fmt.Fprintf(os.Stderr, "If using a remote cluster, ensure the image is pushed to a registry.\n")
	} else {
		fmt.Println("  ✓ Image loaded into cluster")
	}

	// Update config with built image
	cfg.Spec.Image = imageTag

	// Render
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return fmt.Errorf("failed to render: %w", err)
	}

	// Deploy
	fmt.Printf("\nDeploying to %s...\n", targetNS)
	engine := apply.NewEngine(client.Clientset, os.Stdout)
	result, err := engine.Apply(cmd.Context(), bundle)
	if err != nil {
		return err
	}

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  Error: %v\n", e)
		}
	}

	// Wait for rollout
	if bundle.Deployment != nil {
		if err := engine.WaitForRollout(cmd.Context(), targetNS, bundle.Deployment.Name); err != nil {
			return fmt.Errorf("rollout failed: %w", err)
		}
	}

	// Save release to history
	store := release.NewStore(client.Clientset, targetNS, appName)
	revision, err := store.Save(cmd.Context(), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save release history: %v\n", err)
	}

	fmt.Println()
	fmt.Printf("✓ %s is running!\n", appName)
	if revision > 0 {
		fmt.Printf("Release %s saved (rollback available)\n", release.FormatRevision(revision))
	}

	// Stream logs unless disabled
	if !noLogs {
		fmt.Println("\nStreaming logs (Ctrl+C to stop)...")
		fmt.Println()

		// Set up context with cancellation
		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			cancel()
		}()

		// Find pods and stream logs
		pods, err := debug.FindPods(ctx, client.Clientset, targetNS, appName)
		if err != nil {
			return nil // Don't fail if we can't stream logs
		}

		opts := debug.LogsOptions{
			Follow:       true,
			Timestamps:   true,
			TailLines:    50,
			AutoPrevious: true,
			ShowEvents:   true,
		}

		debug.StreamLogs(ctx, client.Clientset, targetNS, pods, opts, os.Stdout)
	}

	return nil
}

func buildImage(ctx context.Context, workDir, tag string) error {
	// Use docker build
	cmd := exec.CommandContext(ctx, "docker", "build", "-t", tag, ".")
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func loadImage(ctx context.Context, kubeContext, imageTag string) error {
	// Detect if it's a kind cluster
	if isKindCluster(kubeContext) {
		// Extract cluster name from context (kind-<name>)
		clusterName := "kind"
		if len(kubeContext) > 5 && kubeContext[:5] == "kind-" {
			clusterName = kubeContext[5:]
		}

		cmd := exec.CommandContext(ctx, "kind", "load", "docker-image", imageTag, "--name", clusterName)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Detect minikube
	if isMinikubeCluster(kubeContext) {
		cmd := exec.CommandContext(ctx, "minikube", "image", "load", imageTag)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// For docker-desktop, the image is already available
	if kubeContext == "docker-desktop" || kubeContext == "docker-for-desktop" {
		return nil
	}

	// For other clusters, we'd need to push to a registry
	// For now, just return nil and let the user handle it
	return nil
}

func isKindCluster(context string) bool {
	// Kind contexts start with "kind-"
	if len(context) >= 5 && context[:5] == "kind-" {
		return true
	}
	// Check if kind is the context name
	return context == "kind"
}

func isMinikubeCluster(context string) bool {
	return context == "minikube"
}

func init() {
	upCmd.Flags().StringP("env", "e", "", "Environment overlay to apply")
	upCmd.Flags().Bool("no-logs", false, "Don't stream logs after deploy")
	rootCmd.AddCommand(upCmd)
}
