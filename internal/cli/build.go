package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bobbyrathoree/kbox/internal/config"
)

// build.go contains shared build helper functions used by up.go and deploy.go.
// The standalone "kbox build" command was removed during the focus sprint.

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

