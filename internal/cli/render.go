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
	configFile, _ := cmd.Flags().GetString("file")

	// Load config
	var loader *config.Loader
	if configFile != "" {
		loader = config.NewLoader(".")
	} else {
		loader = config.NewLoader(".")
	}

	cfg, err := loader.Load()
	if err != nil {
		// Try to infer from Dockerfile
		cfg, err = config.InferFromDockerfile(".")
		if err != nil {
			return fmt.Errorf("no kbox.yaml or Dockerfile found\n  → Create a Dockerfile or run 'kbox init' to get started")
		}
		fmt.Fprintln(os.Stderr, "No kbox.yaml found, inferring from Dockerfile...")
	}

	// Apply environment overlay
	if env != "" {
		cfg = cfg.ForEnvironment(env)
		fmt.Fprintf(os.Stderr, "Using environment: %s\n", env)
	}

	// Check if we have an image
	if cfg.Spec.Image == "" && cfg.Spec.Build == nil {
		return fmt.Errorf("no image specified and no build configuration\n  → Add 'image:' to kbox.yaml or use 'kbox up' for build+deploy")
	}

	// If only build config, use a placeholder image
	if cfg.Spec.Image == "" && cfg.Spec.Build != nil {
		cfg.Spec.Image = fmt.Sprintf("%s:latest", cfg.Metadata.Name)
		fmt.Fprintf(os.Stderr, "Using image: %s (from build config)\n", cfg.Spec.Image)
	}

	// Render
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return fmt.Errorf("failed to render: %w", err)
	}

	// Output YAML
	fmt.Fprintln(os.Stderr) // Blank line before YAML
	return bundle.ToYAML(os.Stdout)
}

func init() {
	renderCmd.Flags().StringP("env", "e", "", "Environment overlay to apply (e.g., dev, staging, prod)")
	renderCmd.Flags().StringP("file", "f", "", "Path to kbox.yaml (default: ./kbox.yaml)")
	rootCmd.AddCommand(renderCmd)
}
