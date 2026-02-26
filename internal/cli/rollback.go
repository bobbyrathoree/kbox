package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/release"
)

func newRollbackCmd() *cobra.Command {
	var (
		namespace  string
		appName    string
		toRevision int
		dryRun     bool
	)

	cmd := &cobra.Command{
		GroupID: "safety",
		Use:     "rollback [app]",
		Short: "Rollback to a previous release",
		Long: `Rollback an application to a previous release.

By default, rolls back to the immediately previous release.
Use --to to specify a specific revision number.

The rollback is saved as a new release, so you can rollback
a rollback if needed.`,
		Example: `  # Rollback to previous release
  kbox rollback

  # Rollback a specific app
  kbox rollback myapp

  # Rollback to a specific revision
  kbox rollback myapp --to 3

  # Preview what would be rolled back
  kbox rollback --dry-run`,
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

			opts := release.RollbackOptions{
				ToRevision: toRevision,
				DryRun:     dryRun,
				Output:     os.Stdout,
			}

			// Show what we're about to do
			store := release.NewStore(client.Clientset, namespace, appName)

			if dryRun {
				fmt.Printf("Dry run - showing what would be rolled back\n\n")
			}

			// Get target release info
			var target *release.Release
			if toRevision > 0 {
				target, err = store.Get(ctx, toRevision)
				if err != nil {
					return fmt.Errorf("cannot find revision %d: %w", toRevision, err)
				}
			} else {
				target, err = store.GetPrevious(ctx)
				if err != nil {
					return fmt.Errorf("cannot rollback: %w", err)
				}
			}

			fmt.Printf("Rolling back %s to revision %s\n", appName, release.FormatRevision(target.Revision))
			fmt.Printf("  Image: %s\n", target.Image)
			fmt.Printf("  Deployed: %s\n\n", formatRelativeTime(target.Timestamp))

			if dryRun {
				fmt.Println("(dry-run) No changes made")
				return nil
			}

			// Perform rollback
			fmt.Println("Applying rollback...")

			result, err := release.Rollback(ctx, client.Clientset, namespace, appName, opts)
			if err != nil {
				return fmt.Errorf("rollback failed: %w", err)
			}

			fmt.Println()
			fmt.Printf("  ✓ %s\n", result.String())
			fmt.Println()
			fmt.Println("Rollback complete!")

			return nil
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")
	cmd.Flags().StringVarP(&appName, "app", "a", "", "Application name (overrides kbox.yaml)")
	cmd.Flags().IntVar(&toRevision, "to", 0, "Revision number to rollback to (default: previous)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be rolled back without making changes")

	return cmd
}

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

func init() {
	rootCmd.AddCommand(newRollbackCmd())
}
