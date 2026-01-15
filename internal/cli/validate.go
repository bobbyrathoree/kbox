package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate kbox.yaml configuration",
	Long: `Validate kbox.yaml syntax and configuration.

This command checks your configuration for:
  - Valid YAML syntax
  - Required fields (metadata.name, spec.image or spec.build)
  - Valid Kubernetes naming conventions
  - Security warnings (e.g., :latest tags)

Examples:
  kbox validate                    # Validate ./kbox.yaml
  kbox validate -f custom.yaml     # Validate specific file
  kbox validate --strict           # Fail on warnings (for CI)`,
	RunE: runValidate,
}

func runValidate(cmd *cobra.Command, args []string) error {
	configFile, _ := cmd.Flags().GetString("file")
	strict, _ := cmd.Flags().GetBool("strict")
	outputFormat := GetOutputFormat(cmd)

	// Load config
	var cfg *config.AppConfig
	var err error

	if configFile != "" {
		loader := config.NewLoader(".")
		cfg, err = loader.LoadFile(configFile)
	} else {
		loader := config.NewLoader(".")
		cfg, err = loader.Load()
	}

	// Prepare result for output
	result := struct {
		Valid    bool     `json:"valid"`
		Strict   bool     `json:"strict"`
		Errors   []string `json:"errors,omitempty"`
		Warnings []string `json:"warnings,omitempty"`
		File     string   `json:"file"`
	}{
		Valid:  err == nil,
		Strict: strict,
		File:   configFile,
	}

	if configFile == "" {
		result.File = "kbox.yaml"
	}

	// Collect errors
	if err != nil {
		result.Errors = []string{err.Error()}
	} else {
		// Check for warnings
		warnings, _ := config.ValidateWithWarnings(cfg)
		result.Warnings = warnings
	}

	// In strict mode, warnings count as failures
	if strict && len(result.Warnings) > 0 {
		result.Valid = false
	}

	// JSON output
	if outputFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
		if !result.Valid {
			return fmt.Errorf("validation failed")
		}
		return nil
	}

	// Text output
	// Check for actual errors first (not strict mode failures)
	if len(result.Errors) > 0 {
		fmt.Printf("Invalid configuration: %s\n", result.File)
		for _, e := range result.Errors {
			fmt.Printf("  Error: %s\n", e)
		}
		return fmt.Errorf("validation failed")
	}

	// Config is syntactically valid
	fmt.Printf("Valid configuration: %s\n", result.File)
	if len(result.Warnings) > 0 {
		for _, w := range result.Warnings {
			fmt.Printf("  Warning: %s\n", w)
		}
		if strict {
			return fmt.Errorf("validation failed: %d warning(s) in strict mode", len(result.Warnings))
		}
	} else {
		fmt.Println("  No warnings")
	}

	return nil
}

func init() {
	validateCmd.Flags().StringP("file", "f", "", "Path to kbox.yaml (default: ./kbox.yaml)")
	validateCmd.Flags().Bool("strict", false, "Treat warnings as errors (for CI pipelines)")
	rootCmd.AddCommand(validateCmd)
}
