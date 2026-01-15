package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status <app>",
	Short: "Show status of an app",
	Long: `Show comprehensive status of an application.

Displays:
  - Deployment status (replicas, strategy, image)
  - Pod status (phase, restarts, age)
  - Container issues (waiting, terminated)
  - Recent events (last hour)

Examples:
  kbox status myapp
  kbox status myapp -n production`,
	Args: cobra.ExactArgs(1),
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	appName := args[0]

	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	ns := client.Namespace
	if namespace != "" {
		ns = namespace
	}

	status, err := debug.GetAppStatus(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	outputFormat := GetOutputFormat(cmd)
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"success": true,
			"app":     appName,
			"status":  status,
		})
	}

	debug.PrintStatus(os.Stdout, status)
	return nil
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
