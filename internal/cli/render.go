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
	redact, _ := cmd.Flags().GetBool("redact")
	configFile, _ := cmd.Flags().GetString("file")
	outputFormat := GetOutputFormat(cmd)
	ciMode := IsCIMode(cmd)

	// If a specific file is provided, load it directly
	if configFile != "" {
		return renderFromFile(cmd, configFile, env, redact, outputFormat, ciMode)
	}

	// Use current directory
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

		// Validate with warnings for security issues
		warnings, err := config.ValidateWithWarnings(cfg)
		if err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
		// Print warnings to stderr (unless JSON output or CI mode suppresses them)
		if !ciMode && outputFormat != "json" {
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}
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

	// Redact secrets if requested
	if redact {
		bundle = redactSecrets(bundle)
	}

	// Output based on format
	if !ciMode && outputFormat != "json" {
		fmt.Fprintln(os.Stderr) // Blank line before YAML
	}

	// JSON output
	if outputFormat == "json" {
		return bundle.ToJSON(os.Stdout)
	}

	// Default YAML output
	return bundle.ToYAML(os.Stdout)
}

// redactSecrets replaces all secret data with redacted placeholders
func redactSecrets(bundle *render.Bundle) *render.Bundle {
	for _, secret := range bundle.Secrets {
		// Redact existing Data entries
		for key := range secret.Data {
			secret.Data[key] = []byte("[REDACTED]")
		}

		// Convert StringData keys to redacted Data entries
		// This ensures the YAML output shows data with [REDACTED] values
		// even when secrets were created with StringData
		if len(secret.StringData) > 0 {
			if secret.Data == nil {
				secret.Data = make(map[string][]byte)
			}
			for key := range secret.StringData {
				secret.Data[key] = []byte("[REDACTED]")
			}
			// Clear StringData since we've moved keys to Data
			secret.StringData = nil
		}
	}
	return bundle
}

// renderFromFile loads and renders a specific config file
func renderFromFile(cmd *cobra.Command, configFile, env string, redact bool, outputFormat string, ciMode bool) error {
	loader := config.NewLoader(".")

	// Load config directly from file
	cfg, err := loader.LoadFile(configFile)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", configFile, err)
	}

	// Validate with warnings for security issues
	warnings, err := config.ValidateWithWarnings(cfg)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	// Print warnings to stderr
	if !ciMode && outputFormat != "json" {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
		}
	}

	// Apply environment overlay
	if env != "" {
		cfg = cfg.ForEnvironment(env)
		if !ciMode {
			fmt.Fprintf(os.Stderr, "Using environment: %s\n", env)
		}
	}

	// Render
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return fmt.Errorf("failed to render: %w", err)
	}

	// Redact secrets if requested
	if redact {
		bundle = redactSecrets(bundle)
	}

	// Output
	if !ciMode && outputFormat != "json" {
		fmt.Fprintln(os.Stderr)
	}

	if outputFormat == "json" {
		return bundle.ToJSON(os.Stdout)
	}
	return bundle.ToYAML(os.Stdout)
}

func init() {
	renderCmd.Flags().StringP("env", "e", "", "Environment overlay to apply (e.g., dev, staging, prod)")
	renderCmd.Flags().StringP("file", "f", "", "Path to kbox.yaml (default: ./kbox.yaml)")
	renderCmd.Flags().Bool("redact", false, "Redact secret values in output (for security)")
	rootCmd.AddCommand(renderCmd)
}
