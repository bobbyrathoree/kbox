package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build container image",
	Long: `Build a container image from Dockerfile.

Supports multiple tag strategies and registry push:
  - kbox-timestamp: Uses Unix timestamp (default)
  - git-sha: Uses short git commit SHA
  - git-tag: Uses exact git tag (fails if no tag)
  - latest: Always uses "latest" tag

Examples:
  kbox build                                 # Build with timestamp tag
  kbox build --tag git-sha                   # Build with git commit SHA
  kbox build --push --registry ecr.io/myapp  # Build and push to registry
  kbox build --tag latest --push             # Build and push with "latest" tag`,
	RunE: runBuild,
}

func runBuild(cmd *cobra.Command, args []string) error {
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

	// Try to load config for build settings
	loader := config.NewLoader(workDir)
	cfg, err := loader.Load()
	if err != nil {
		// Infer from Dockerfile if no config
		cfg, err = config.InferFromDockerfile(workDir)
		if err != nil {
			return fmt.Errorf("no kbox.yaml or Dockerfile found in %s", workDir)
		}
	}

	// Use config name if available
	if cfg.Metadata.Name != "" {
		appName = cfg.Metadata.Name
	}

	// Determine tag strategy (CLI flag > config > default)
	if tagStrategy == "" && cfg.Spec.Build != nil && cfg.Spec.Build.Tag != "" {
		tagStrategy = cfg.Spec.Build.Tag
	}
	if tagStrategy == "" {
		tagStrategy = config.TagKboxTimestamp
	}

	// Determine registry (CLI flag > env var > config)
	if registry == "" {
		registry = os.Getenv("KBOX_REGISTRY")
	}
	if registry == "" && cfg.Spec.Build != nil && cfg.Spec.Build.Push != nil {
		registry = cfg.Spec.Build.Push.Registry
	}

	// Generate image tag
	imageTag, err := generateImageTag(appName, tagStrategy, registry)
	if err != nil {
		return fmt.Errorf("failed to generate image tag: %w", err)
	}

	fmt.Printf("Building image: %s\n", imageTag)

	// Build the image
	if err := buildImageWithConfig(cmd.Context(), workDir, imageTag, cfg.Spec.Build); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	fmt.Println("  ✓ Image built successfully")

	// Push if requested
	if pushFlag || (!noPushFlag && registry != "" && shouldPushToRegistry(cfg, pushFlag, noPushFlag)) {
		if registry == "" {
			return fmt.Errorf("--registry is required for push")
		}

		fmt.Printf("Pushing image: %s\n", imageTag)
		if err := pushImage(cmd.Context(), imageTag); err != nil {
			return fmt.Errorf("push failed: %w", err)
		}
		fmt.Println("  ✓ Image pushed successfully")
	}

	fmt.Printf("\nImage: %s\n", imageTag)
	return nil
}

// generateImageTag creates an image tag based on the strategy
func generateImageTag(appName, strategy, registry string) (string, error) {
	var tagSuffix string

	switch strategy {
	case config.TagGitSha:
		out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
		if err != nil {
			return "", fmt.Errorf("failed to get git SHA: %w (is this a git repository?)", err)
		}
		tagSuffix = strings.TrimSpace(string(out))

	case config.TagGitTag:
		out, err := exec.Command("git", "describe", "--tags", "--exact-match").Output()
		if err != nil {
			return "", fmt.Errorf("failed to get git tag: %w (no tag on current commit?)", err)
		}
		tagSuffix = strings.TrimSpace(string(out))

	case config.TagLatest:
		tagSuffix = "latest"

	default: // kbox-timestamp
		tagSuffix = fmt.Sprintf("kbox-%d", time.Now().Unix())
	}

	if registry != "" {
		return fmt.Sprintf("%s:%s", registry, tagSuffix), nil
	}
	return fmt.Sprintf("%s:%s", appName, tagSuffix), nil
}

// buildImageWithConfig builds the Docker image with config options
func buildImageWithConfig(ctx context.Context, workDir, tag string, buildCfg *config.BuildConfig) error {
	args := []string{"build", "-t", tag}

	// Apply build config options
	if buildCfg != nil {
		if buildCfg.Dockerfile != "" {
			args = append(args, "-f", buildCfg.Dockerfile)
		}
		if buildCfg.Target != "" {
			args = append(args, "--target", buildCfg.Target)
		}
		for k, v := range buildCfg.Args {
			args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Add context
	context := "."
	if buildCfg != nil && buildCfg.Context != "" {
		context = buildCfg.Context
	}
	args = append(args, context)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// pushImage pushes the image to the registry
func pushImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "push", imageTag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// shouldPushToRegistry determines if the image should be pushed based on config and flags
func shouldPushToRegistry(cfg *config.AppConfig, pushFlag, noPushFlag bool) bool {
	// Explicit flags take precedence
	if pushFlag {
		return true
	}
	if noPushFlag {
		return false
	}

	// Check environment variable
	if envPush := os.Getenv("KBOX_PUSH"); envPush != "" {
		return envPush == "true" || envPush == "1"
	}

	// Check config
	if cfg.Spec.Build != nil && cfg.Spec.Build.Push != nil {
		switch cfg.Spec.Build.Push.Enabled {
		case config.PushAlways:
			return true
		case config.PushNever:
			return false
		}
	}

	// Auto mode: don't push unless explicitly requested via flag or env
	return false
}

// determinePushBehavior determines if push should happen based on cluster context
func determinePushBehavior(kubeContext string, pushFlag, noPushFlag bool, cfg *config.AppConfig) (bool, error) {
	// Explicit flags take precedence
	if pushFlag {
		return true, nil
	}
	if noPushFlag {
		return false, nil
	}

	// Check environment variable
	if envPush := os.Getenv("KBOX_PUSH"); envPush != "" {
		return envPush == "true" || envPush == "1", nil
	}

	// Check config
	if cfg.Spec.Build != nil && cfg.Spec.Build.Push != nil {
		switch cfg.Spec.Build.Push.Enabled {
		case config.PushAlways:
			return true, nil
		case config.PushNever:
			// For remote clusters with never, warn but continue
			if !isLocalCluster(kubeContext) {
				fmt.Fprintf(os.Stderr, "Warning: push is disabled but cluster appears to be remote. Image may not be available.\n")
			}
			return false, nil
		}
	}

	// Auto mode: push for remote clusters, load locally for local clusters
	if isLocalCluster(kubeContext) {
		return false, nil
	}

	// Remote cluster - need to push
	return true, nil
}

func init() {
	buildCmd.Flags().Bool("push", false, "Push image to registry after build")
	buildCmd.Flags().Bool("no-push", false, "Force local build only, skip registry push")
	buildCmd.Flags().String("registry", "", "Target registry (e.g., ecr.io/myapp)")
	buildCmd.Flags().String("tag", "", "Tag strategy: kbox-timestamp, git-sha, git-tag, latest")
	rootCmd.AddCommand(buildCmd)
}
