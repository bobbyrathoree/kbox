package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/release"
)

func newHistoryCmd() *cobra.Command {
	var (
		namespace string
		appName   string
	)

	cmd := &cobra.Command{
		Use:   "history [app]",
		Short: "Show release history for an application",
		Long: `Show the deployment history for an application.

Each deployment creates a release that can be rolled back to.
The history shows the revision number, timestamp, and image deployed.`,
		Example: `  # Show history for app in kbox.yaml
  kbox history

  # Show history for a specific app
  kbox history myapp

  # Show history in a specific namespace
  kbox history myapp -n production`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load config to get defaults
			loader := config.NewLoader(".")
			cfg, _ := loader.Load() // Ignore error - might not have kbox.yaml

			// Determine app name
			if len(args) > 0 {
				appName = args[0]
			} else if appName == "" && cfg != nil {
				appName = cfg.Metadata.Name
			}
			if appName == "" {
				return fmt.Errorf("app name required (specify as argument or use kbox.yaml)")
			}

			// Determine namespace
			if namespace == "" {
				if cfg != nil && cfg.Metadata.Namespace != "" {
					namespace = cfg.Metadata.Namespace
				} else {
					namespace = "default"
				}
			}

			// Get K8s client
			client, err := k8s.NewClient(k8s.ClientOptions{})
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Get release history
			store := release.NewStore(client.Clientset, namespace, appName)
			releases, err := store.List(ctx)
			if err != nil {
				return fmt.Errorf("failed to get release history: %w", err)
			}

			if len(releases) == 0 {
				fmt.Printf("No releases found for %s in namespace %s\n", appName, namespace)
				return nil
			}

			// Print header
			fmt.Printf("Release history for %s (namespace: %s)\n\n", appName, namespace)
			fmt.Printf("%-10s %-25s %s\n", "REVISION", "DEPLOYED", "IMAGE")
			fmt.Printf("%-10s %-25s %s\n", "--------", "--------", "-----")

			// Print releases (newest first)
			for i := len(releases) - 1; i >= 0; i-- {
				r := releases[i]
				relativeTime := formatRelativeTime(r.Timestamp)
				fmt.Printf("%-10s %-25s %s\n",
					release.FormatRevision(r.Revision),
					relativeTime,
					truncateImage(r.Image, 50))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	cmd.Flags().StringVarP(&appName, "app", "a", "", "Application name (overrides kbox.yaml)")

	return cmd
}

func init() {
	rootCmd.AddCommand(newHistoryCmd())
}

// formatRelativeTime returns a human-readable relative time
func formatRelativeTime(t time.Time) string {
	now := time.Now().UTC()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("Jan 02, 2006 15:04")
	}
}

// truncateImage truncates long image names for display
func truncateImage(image string, maxLen int) string {
	if len(image) <= maxLen {
		return image
	}
	return image[:maxLen-3] + "..."
}
