package cli

import (
	"fmt"
	"os"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/render"
	"github.com/spf13/cobra"
)

var renderCmd = &cobra.Command{
	Use:   "render",
	Short: "Render Kubernetes manifests from kbox.yaml",
	Long: `Render Kubernetes manifests without applying them.

This command generates YAML that would be applied to the cluster.
Useful for previewing changes or piping to kubectl.

Examples:
  kbox render                    # Render with default environment
  kbox render -e prod            # Render with prod environment overlay
  kbox render -e dev | kubectl apply -f -  # Pipe to kubectl`,
	RunE: runRender,
}

func runRender(cmd *cobra.Command, args []string) error {
	env, _ := cmd.Flags().GetString("env")
	ciMode := IsCIMode(cmd)

	loader := config.NewLoader(".")

	// Check if this is a multi-service config
	isMulti, err := loader.IsMultiService()
	if err != nil {
		// Try to infer from Dockerfile
		cfg, err := config.InferFromDockerfile(".")
		if err != nil {
			return fmt.Errorf("no kbox.yaml or Dockerfile found\n  → Create a Dockerfile or run 'kbox init' to get started")
		}
		if !ciMode {
			fmt.Fprintln(os.Stderr, "No kbox.yaml found, inferring from Dockerfile...")
		}

		// Render inferred config
		renderer := render.New(cfg)
		bundle, err := renderer.Render()
		if err != nil {
			return fmt.Errorf("failed to render: %w", err)
		}

		if !ciMode {
			fmt.Fprintln(os.Stderr)
		}
		return bundle.ToYAML(os.Stdout)
	}

	var bundle *render.Bundle

	if isMulti {
		// Handle multi-service config
		multiCfg, err := loader.LoadMultiService()
		if err != nil {
			return fmt.Errorf("failed to load kbox.yaml: %w", err)
		}

		// Apply environment overlay
		if env != "" {
			multiCfg = multiCfg.ForEnvironment(env)
			if !ciMode {
				fmt.Fprintf(os.Stderr, "Using environment: %s\n", env)
			}
		}

		// Render using multi-service renderer
		renderer := render.NewMultiService(multiCfg)
		bundle, err = renderer.Render()
		if err != nil {
			return fmt.Errorf("failed to render: %w", err)
		}
	} else {
		// Handle single-service config
		cfg, err := loader.Load()
		if err != nil {
			return fmt.Errorf("failed to load kbox.yaml: %w", err)
		}

		// Apply environment overlay
		if env != "" {
			cfg = cfg.ForEnvironment(env)
			if !ciMode {
				fmt.Fprintf(os.Stderr, "Using environment: %s\n", env)
			}
		}

		// Check if we have an image
		if cfg.Spec.Image == "" && cfg.Spec.Build == nil {
			return fmt.Errorf("no image specified and no build configuration\n  → Add 'image:' to kbox.yaml or use 'kbox up' for build+deploy")
		}

		// If only build config, use a placeholder image
		if cfg.Spec.Image == "" && cfg.Spec.Build != nil {
			cfg.Spec.Image = fmt.Sprintf("%s:latest", cfg.Metadata.Name)
			if !ciMode {
				fmt.Fprintf(os.Stderr, "Using image: %s (from build config)\n", cfg.Spec.Image)
			}
		}

		// Render
		renderer := render.New(cfg)
		bundle, err = renderer.Render()
		if err != nil {
			return fmt.Errorf("failed to render: %w", err)
		}
	}

	// Output YAML
	if !ciMode {
		fmt.Fprintln(os.Stderr) // Blank line before YAML
	}
	return bundle.ToYAML(os.Stdout)
}

func init() {
	renderCmd.Flags().StringP("env", "e", "", "Environment overlay to apply (e.g., dev, staging, prod)")
	renderCmd.Flags().StringP("file", "f", "", "Path to kbox.yaml (default: ./kbox.yaml)")
	rootCmd.AddCommand(renderCmd)
}
