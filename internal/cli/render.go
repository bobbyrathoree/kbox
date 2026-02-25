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
Useful for previewing changes, piping to kubectl, or GitOps workflows.

Examples:
  kbox render                           # Render with default environment
  kbox render -e prod                   # Render with prod environment overlay
  kbox render -e dev | kubectl apply -f -  # Pipe to kubectl
  kbox render --out manifests.yaml      # Write to file (GitOps)
  kbox render -e prod --out deploy/prod.yaml  # Environment-specific file`,
	RunE: runRender,
}

func runRender(cmd *cobra.Command, args []string) error {
	env, _ := cmd.Flags().GetString("env")
	redact, _ := cmd.Flags().GetBool("redact")
	configFile, _ := cmd.Flags().GetString("file")
	showSummary, _ := cmd.Flags().GetBool("summary")
	outputFile, _ := cmd.Flags().GetString("out")
	outputFormat := GetOutputFormat(cmd)
	ciMode := IsCIMode(cmd)

	// If a specific file is provided, load it directly
	if configFile != "" {
		return renderFromFile(cmd, configFile, env, redact, showSummary, outputFile, outputFormat, ciMode)
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
			multiCfg, err = multiCfg.ForEnvironment(env)
			if err != nil {
				return err
			}
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
			cfg, err = cfg.ForEnvironment(env)
			if err != nil {
				return err
			}
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

	// Show summary if requested
	if showSummary {
		printBundleSummary(bundle)
		return nil
	}

	// Determine output writer
	var writer *os.File = os.Stdout
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()
		writer = f
	}

	// Output based on format
	if !ciMode && outputFormat != "json" && outputFile == "" {
		fmt.Fprintln(os.Stderr) // Blank line before YAML
	}

	// JSON output
	if outputFormat == "json" {
		if err := bundle.ToJSON(writer); err != nil {
			return err
		}
	} else {
		// Default YAML output
		if err := bundle.ToYAML(writer); err != nil {
			return err
		}
	}

	// Print confirmation when writing to file
	if outputFile != "" && !ciMode {
		fmt.Fprintf(os.Stderr, "Wrote manifests to %s\n", outputFile)
	}

	return nil
}

// printBundleSummary prints a summary of resources in the bundle
func printBundleSummary(bundle *render.Bundle) {
	total := len(bundle.AllObjects())
	fmt.Printf("Resource Summary:\n")
	fmt.Printf("  Total resources: %d\n\n", total)

	// Core resources
	if bundle.Deployment != nil {
		fmt.Printf("  Deployment:      %s\n", bundle.Deployment.Name)
	}
	if len(bundle.Services) > 0 {
		fmt.Printf("  Services:        %d\n", len(bundle.Services))
		for _, svc := range bundle.Services {
			fmt.Printf("    - %s\n", svc.Name)
		}
	}
	if len(bundle.Secrets) > 0 {
		fmt.Printf("  Secrets:         %d\n", len(bundle.Secrets))
	}
	if bundle.ServiceAccount != nil {
		fmt.Printf("  ServiceAccount:  %s\n", bundle.ServiceAccount.Name)
	}
	if bundle.HPA != nil {
		fmt.Printf("  HPA:             %s (min: %d, max: %d)\n",
			bundle.HPA.Name,
			*bundle.HPA.Spec.MinReplicas,
			bundle.HPA.Spec.MaxReplicas)
	}
	if bundle.PDB != nil {
		fmt.Printf("  PDB:             %s\n", bundle.PDB.Name)
	}
	if len(bundle.NetworkPolicies) > 0 {
		fmt.Printf("  NetworkPolicies: %d\n", len(bundle.NetworkPolicies))
	}

	// Ingresses
	if len(bundle.Ingresses) > 0 {
		fmt.Printf("  Ingresses:       %d\n", len(bundle.Ingresses))
		for _, ing := range bundle.Ingresses {
			host := "(wildcard)"
			if len(ing.Spec.Rules) > 0 && ing.Spec.Rules[0].Host != "" {
				host = ing.Spec.Rules[0].Host
			}
			fmt.Printf("    - %s (%s)\n", ing.Name, host)
		}
	}
	if len(bundle.ServiceMonitors) > 0 {
		fmt.Printf("  ServiceMonitors: %d\n", len(bundle.ServiceMonitors))
	}
	if len(bundle.ExternalSecrets) > 0 {
		fmt.Printf("  ExternalSecrets: %d\n", len(bundle.ExternalSecrets))
	}

	// Dependencies
	if len(bundle.StatefulSets) > 0 {
		fmt.Printf("\n  Dependencies:\n")
		for _, ss := range bundle.StatefulSets {
			fmt.Printf("    - %s (StatefulSet)\n", ss.Name)
		}
	}
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
func renderFromFile(cmd *cobra.Command, configFile, env string, redact, showSummary bool, outputFile, outputFormat string, ciMode bool) error {
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
		cfg, err = cfg.ForEnvironment(env)
		if err != nil {
			return err
		}
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

	// Show summary if requested
	if showSummary {
		printBundleSummary(bundle)
		return nil
	}

	// Determine output writer
	var writer *os.File = os.Stdout
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()
		writer = f
	}

	// Output
	if !ciMode && outputFormat != "json" && outputFile == "" {
		fmt.Fprintln(os.Stderr)
	}

	if outputFormat == "json" {
		if err := bundle.ToJSON(writer); err != nil {
			return err
		}
	} else {
		if err := bundle.ToYAML(writer); err != nil {
			return err
		}
	}

	// Print confirmation when writing to file
	if outputFile != "" && !ciMode {
		fmt.Fprintf(os.Stderr, "Wrote manifests to %s\n", outputFile)
	}

	return nil
}

func init() {
	renderCmd.Flags().StringP("env", "e", "", "Environment overlay to apply (e.g., dev, staging, prod)")
	renderCmd.Flags().StringP("file", "f", "", "Path to kbox.yaml (default: ./kbox.yaml)")
	renderCmd.Flags().String("out", "", "Output file path (default: stdout)")
	renderCmd.Flags().Bool("redact", false, "Redact secret values in output (for security)")
	renderCmd.Flags().Bool("summary", false, "Show resource summary instead of full YAML")
	rootCmd.AddCommand(renderCmd)
}
