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
	GroupID: "core",
	Use:   "up",
	Short: "Build and deploy with zero config",
	Long: `Build and deploy an application with minimal configuration.

Works with just a Dockerfile:
  1. Detects settings from Dockerfile (EXPOSE port, etc.)
  2. Builds a container image
  3. Loads it into your local cluster (kind/minikube) or pushes to registry
  4. Deploys and streams logs

If kbox.yaml exists, it will use that for additional configuration.

Push Behavior:
  For local clusters (kind, minikube, docker-desktop): Images are loaded directly
  For remote clusters (EKS, GKE, etc.): Images are pushed to registry (--registry required)

Tag Strategies:
  kbox-timestamp  Uses Unix timestamp (default)
  git-sha         Uses short git commit SHA
  git-tag         Uses exact git tag
  latest          Always uses "latest" tag

Examples:
  kbox up                                     # Build and deploy current directory
  kbox up -e dev                              # With environment overlay
  kbox up --no-logs                           # Deploy without streaming logs
  kbox up --push --registry ecr.io/myapp      # Build, push, and deploy
  kbox up --no-push                           # Force local load (no push)
  kbox up --tag git-sha                       # Use git commit SHA as tag`,
	RunE: runUp,
}

func runUp(cmd *cobra.Command, args []string) error {
	startTime := time.Now()

	env, _ := cmd.Flags().GetString("env")
	noLogs, _ := cmd.Flags().GetBool("no-logs")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")
	pushFlag, _ := cmd.Flags().GetBool("push")
	noPushFlag, _ := cmd.Flags().GetBool("no-push")
	registry, _ := cmd.Flags().GetString("registry")
	tagStrategy, _ := cmd.Flags().GetString("tag")

	// Validate conflicting flags
	if pushFlag && noPushFlag {
		return fmt.Errorf("cannot specify both --push and --no-push")
	}

	// Validate tag strategy if provided
	if tagStrategy != "" {
		validStrategies := map[string]bool{
			config.TagKboxTimestamp: true,
			config.TagGitSha:        true,
			config.TagGitTag:        true,
			config.TagLatest:        true,
		}
		if !validStrategies[tagStrategy] {
			return fmt.Errorf("invalid tag strategy %q, must be one of: kbox-timestamp, git-sha, git-tag, latest", tagStrategy)
		}
	}

	// Get working directory
	workDir, err := os.Getwd()
	if err != nil {
		return err
	}
	appName := filepath.Base(workDir)

	// Try to load config, or infer from Dockerfile
	step("Loading config...")
	loader := config.NewLoader(workDir)
	cfg, err := loader.Load()
	if err != nil {
		// Infer from Dockerfile
		cfg, err = config.InferFromDockerfile(workDir)
		if err != nil {
			return fmt.Errorf("no kbox.yaml or Dockerfile found in %s\n  → Create a Dockerfile or run 'kbox init'", workDir)
		}
		dimInfo("No kbox.yaml found — using zero-config mode")
		dimInfo(fmt.Sprintf("Detected: Dockerfile (port %d)", cfg.Spec.Port))
		dimInfo("Tip: Run 'kbox init' to customize")
		fmt.Println()
	} else {
		success("Config loaded")
	}

	// Use config name if available (overrides directory name)
	if cfg.Metadata.Name != "" {
		appName = cfg.Metadata.Name
	}

	// Apply environment overlay
	if env != "" {
		cfg, err = cfg.ForEnvironment(env)
		if err != nil {
			return err
		}
		dimInfo(fmt.Sprintf("Environment: %s", env))
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
		return fmt.Errorf("failed to connect to cluster: %w\n  → Run 'kbox doctor' to diagnose connection issues", err)
	}

	targetNS := cfg.Metadata.Namespace
	if targetNS == "" {
		targetNS = client.Namespace
	}

	// Determine tag strategy (CLI flag > config > default)
	if tagStrategy == "" && cfg.Spec.Build != nil && cfg.Spec.Build.Tag != "" {
		tagStrategy = cfg.Spec.Build.Tag
	}

	// Determine registry (CLI flag > env var > config)
	if registry == "" {
		registry = os.Getenv("KBOX_REGISTRY")
	}
	if registry == "" && cfg.Spec.Build != nil && cfg.Spec.Build.Push != nil {
		registry = cfg.Spec.Build.Push.Registry
	}

	// Determine if we should push based on cluster type
	shouldPush, err := determinePushBehavior(client.Context, pushFlag, noPushFlag, cfg)
	if err != nil {
		return err
	}

	// If pushing to remote, registry is required
	if shouldPush && registry == "" {
		return fmt.Errorf("--registry is required when pushing to remote clusters\n  → Use --registry <registry-url> or set KBOX_REGISTRY env var")
	}

	// Generate image tag
	imageTag, err := generateImageTag(appName, tagStrategy, registry)
	if err != nil {
		return fmt.Errorf("failed to generate image tag: %w", err)
	}

	// Build image
	step("Building image...")
	buildStart := time.Now()
	if err := buildImageWithConfig(cmd.Context(), workDir, imageTag, cfg.Spec.Build); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	success(fmt.Sprintf("Built in %s", time.Since(buildStart).Round(time.Millisecond*100)))

	// Either push to registry or load into local cluster
	if shouldPush {
		step("Pushing image to registry...")
		pushStart := time.Now()
		if err := pushImage(cmd.Context(), imageTag); err != nil {
			return fmt.Errorf("push failed: %w", err)
		}
		success(fmt.Sprintf("Pushed in %s", time.Since(pushStart).Round(time.Millisecond*100)))
	} else {
		step("Loading image into cluster...")
		loadStart := time.Now()
		// Load into cluster (detect kind/minikube)
		if err := loadImage(cmd.Context(), client.Context, imageTag); err != nil {
			warn(fmt.Sprintf("Failed to load image into cluster: %v", err))
			warn("If using a remote cluster, ensure the image is pushed to a registry.")
		} else {
			success(fmt.Sprintf("Loaded in %s", time.Since(loadStart).Round(time.Millisecond*100)))
		}
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
	fmt.Println()
	step(fmt.Sprintf("Deploying to %s...", targetNS))
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
		step("Waiting for pods...")
		if err := engine.WaitForRollout(cmd.Context(), targetNS, bundle.Deployment.Name); err != nil {
			return fmt.Errorf("rollout failed: %w", err)
		}
	}

	// Save release to history
	store := release.NewStore(client.Clientset, targetNS, appName)
	revision, err := store.Save(cmd.Context(), cfg)
	if err != nil {
		warn(fmt.Sprintf("Failed to save release history: %v", err))
	}

	// Print summary
	fmt.Println()
	success(fmt.Sprintf("App running! (%s total)", time.Since(startTime).Round(time.Millisecond*100)))
	dimInfo(fmt.Sprintf("Namespace: %s", targetNS))
	dimInfo(fmt.Sprintf("Image: %s", imageTag))
	if revision > 0 {
		dimInfo(fmt.Sprintf("Release %s saved (rollback available)", release.FormatRevision(revision)))
	}
	fmt.Println()

	// Stream logs unless disabled
	if !noLogs {
		step("Streaming logs (Ctrl+C to stop)")
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

func isDockerDesktop(kubeContext string) bool {
	return kubeContext == "docker-desktop" || kubeContext == "docker-for-desktop"
}

func isLocalCluster(kubeContext string) bool {
	return isKindCluster(kubeContext) ||
		isMinikubeCluster(kubeContext) ||
		isDockerDesktop(kubeContext)
}

func init() {
	upCmd.Flags().StringP("env", "e", "", "Environment overlay to apply")
	upCmd.Flags().Bool("no-logs", false, "Don't stream logs after deploy")
	upCmd.Flags().Bool("push", false, "Push image to registry (required for remote clusters)")
	upCmd.Flags().Bool("no-push", false, "Force local load, don't push to registry")
	upCmd.Flags().String("registry", "", "Target registry for push (e.g., ecr.io/myapp)")
	upCmd.Flags().String("tag", "", "Tag strategy: kbox-timestamp, git-sha, git-tag, latest")
	rootCmd.AddCommand(upCmd)
}
