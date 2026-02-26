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
	Short: "The fastest way to deploy to Kubernetes",
	Long: `kbox — deploy to Kubernetes in seconds, not hours.

Just have a Dockerfile? That's all you need:

  kbox up                    Build, deploy, stream logs
  kbox add postgres          Add a database with one command
  kbox down                  Clean teardown

Get started:
  kbox up                    # Zero-config deploy (just needs Dockerfile)
  kbox init                  # Create kbox.yaml for more control
  kbox doctor                # Check your setup`,
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

// exactArgs returns a PositionalArgs that reports a helpful error when wrong number of args provided
func exactArgs(n int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return fmt.Errorf("missing required argument: %s\n\nUsage:\n  %s", usage, cmd.UseLine())
		}
		return nil
	}
}

// minimumArgs returns a PositionalArgs that reports a helpful error when too few args provided
func minimumArgs(n int, usage string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < n {
			return fmt.Errorf("missing required argument: %s\n\nUsage:\n  %s", usage, cmd.UseLine())
		}
		return nil
	}
}

func init() {
	// Command groups for organized --help
	rootCmd.AddGroup(
		&cobra.Group{ID: "core", Title: "Core Commands:"},
		&cobra.Group{ID: "setup", Title: "Setup:"},
		&cobra.Group{ID: "observe", Title: "Observe:"},
		&cobra.Group{ID: "safety", Title: "Safety:"},
	)

	// Global flags
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
