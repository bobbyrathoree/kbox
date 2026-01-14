package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
  kbox up       Zero-config deploy (just needs a Dockerfile)
  kbox deploy   Deploy with full control via kbox.yaml
  kbox logs     Logs with K8s events interleaved
  kbox shell    Shell into any container (even distroless)
  kbox rollback Fast escape hatch

Get started:
  kbox up              # Deploy current directory (zero config)
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
}
