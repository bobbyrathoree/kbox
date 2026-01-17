package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/tui"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard [app-name]",
	Short: "Launch interactive monitoring dashboard",
	Long: `Launch a terminal UI for real-time monitoring of your deployment.

Features:
  - Live deployment status and pod health (2s refresh)
  - CPU/Memory metrics with sparkline graphs
  - Real-time log streaming from all pods
  - Keyboard shortcuts for common actions

Controls:
  Tab       Switch between pods and logs panes
  r         Restart deployment
  l         Toggle fullscreen logs
  ↑/↓       Scroll logs
  ?         Show help
  q         Quit

Examples:
  kbox dashboard              # Auto-detect from kbox.yaml
  kbox dashboard myapp        # Monitor specific app
  kbox dashboard -n staging   # Monitor in specific namespace`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDashboard,
}

func runDashboard(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	var appName string

	// Get app name from args or kbox.yaml
	if len(args) > 0 {
		appName = args[0]
	} else {
		// Try to load from kbox.yaml
		loader := config.NewLoader(".")
		isMulti, err := loader.IsMultiService()
		if err == nil {
			if isMulti {
				cfg, err := loader.LoadMultiService()
				if err == nil {
					appName = cfg.Metadata.Name
					if namespace == "" {
						namespace = cfg.Metadata.Namespace
					}
				}
			} else {
				cfg, err := loader.Load()
				if err == nil {
					appName = cfg.Metadata.Name
					if namespace == "" {
						namespace = cfg.Metadata.Namespace
					}
				}
			}
		}
	}

	if appName == "" {
		return fmt.Errorf("no app name specified\n\n" +
			"Usage:\n" +
			"  kbox dashboard <app-name>    # Specify app name\n" +
			"  kbox dashboard               # Auto-detect from kbox.yaml (run in project directory)")
	}

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w\n  → Run 'kbox doctor' to diagnose connection issues", err)
	}

	ns := client.Namespace
	if namespace != "" {
		ns = namespace
	}

	// Create and run the TUI
	model := tui.NewDashboard(client, appName, ns)
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("dashboard error: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}
