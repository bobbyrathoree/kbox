package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/output"
)

var (
	// Version information set at build time
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "kbox",
	Short: "The Bun of Kubernetes app workflows",
	Long: `kbox - what kubectl should have been for app developers

A single tool that turns K8s app lifecycle into simple commands:
  kbox up       Build + deploy + stream logs (for development)
  kbox deploy   Deploy pre-built images (for CI/CD pipelines)
  kbox logs     Logs with K8s events interleaved
  kbox shell    Shell into any container (even distroless)
  kbox rollback Fast escape hatch

Which command should I use?
  kbox up       → Use during development. Builds from Dockerfile, deploys, streams logs.
  kbox deploy   → Use in CI/CD. Requires pre-built image in kbox.yaml or registry.

Get started:
  kbox up              # Build + deploy current directory (just needs Dockerfile)
  kbox init            # Create kbox.yaml for more control
  kbox doctor          # Check your setup`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

func init() {
	// Global flags can be added here
	rootCmd.PersistentFlags().StringP("namespace", "n", "", "Kubernetes namespace (default: from kubeconfig)")
	rootCmd.PersistentFlags().StringP("context", "", "", "Kubernetes context (default: current context)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")

	// CI mode flags
	rootCmd.PersistentFlags().Bool("ci", false, "CI mode: no prompts, clean exit codes, minimal output")
	rootCmd.PersistentFlags().StringP("output", "o", "text", "Output format: text, json")
}

// IsCIMode returns true if CI mode is enabled via flag or environment
func IsCIMode(cmd *cobra.Command) bool {
	ci, _ := cmd.Flags().GetBool("ci")
	if ci {
		return true
	}
	// Also check environment variables
	if os.Getenv("CI") == "true" || os.Getenv("KBOX_CI") == "true" {
		return true
	}
	return false
}

// GetOutputFormat returns the output format (text or json)
func GetOutputFormat(cmd *cobra.Command) string {
	format, _ := cmd.Flags().GetString("output")
	if format == "" {
		format = "text"
	}
	return format
}

// NewOutputWriter creates an output writer based on command flags
func NewOutputWriter(cmd *cobra.Command) *output.Writer {
	return output.NewWriter(os.Stdout, GetOutputFormat(cmd), IsCIMode(cmd))
}
