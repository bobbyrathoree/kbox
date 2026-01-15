package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Remove app resources from cluster",
	Long: `Delete all Kubernetes resources created by kbox for this app.

This removes: Deployment, Service, ConfigMaps, Secrets, StatefulSets.
PersistentVolumeClaims are NOT deleted by default (to preserve data).

Examples:
  kbox down              # Delete resources in default namespace
  kbox down -n staging   # Delete from specific namespace
  kbox down --all        # Also delete PVCs (data loss warning!)
  kbox down --force      # Skip confirmation prompt`,
	RunE: runDown,
}

func runDown(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")
	force, _ := cmd.Flags().GetBool("force")
	all, _ := cmd.Flags().GetBool("all")
	ciMode := IsCIMode(cmd)

	// Load config to get app name
	loader := config.NewLoader(".")
	var appName string

	isMulti, err := loader.IsMultiService()
	if err != nil {
		return fmt.Errorf("no kbox.yaml found\n  → Run this command in a directory with kbox.yaml")
	}

	if isMulti {
		cfg, err := loader.LoadMultiService()
		if err != nil {
			return fmt.Errorf("failed to load kbox.yaml: %w", err)
		}
		appName = cfg.Metadata.Name
		if namespace == "" {
			namespace = cfg.Metadata.Namespace
		}
	} else {
		cfg, err := loader.Load()
		if err != nil {
			return fmt.Errorf("failed to load kbox.yaml: %w", err)
		}
		appName = cfg.Metadata.Name
		if namespace == "" {
			namespace = cfg.Metadata.Namespace
		}
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w\n  → Run 'kbox doctor' to diagnose connection issues", err)
	}

	targetNS := namespace
	if targetNS == "" {
		targetNS = client.Namespace
	}

	// Confirmation (unless force or CI mode)
	if !force && !ciMode {
		fmt.Printf("This will delete all resources for %q in namespace %q.\n", appName, targetNS)
		if all {
			fmt.Println("\n  WARNING: --all flag will also delete PersistentVolumeClaims (data loss!)")
		}
		fmt.Print("\nContinue? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
		fmt.Println()
	}

	ctx := cmd.Context()
	selector := fmt.Sprintf("app=%s", appName)
	deleteOpts := metav1.DeleteOptions{}
	listOpts := metav1.ListOptions{LabelSelector: selector}

	var deleted []string
	var errors []error

	// Delete in reverse dependency order

	// 1. Deployments
	deps, err := client.Clientset.AppsV1().Deployments(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, dep := range deps.Items {
			if err := client.Clientset.AppsV1().Deployments(targetNS).Delete(ctx, dep.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("Deployment/%s", dep.Name))
				fmt.Printf("  ✓ Deleted Deployment/%s\n", dep.Name)
			} else {
				errors = append(errors, fmt.Errorf("Deployment/%s: %w", dep.Name, err))
			}
		}
	}

	// 2. StatefulSets
	statefulsets, err := client.Clientset.AppsV1().StatefulSets(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, ss := range statefulsets.Items {
			if err := client.Clientset.AppsV1().StatefulSets(targetNS).Delete(ctx, ss.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("StatefulSet/%s", ss.Name))
				fmt.Printf("  ✓ Deleted StatefulSet/%s\n", ss.Name)
			} else {
				errors = append(errors, fmt.Errorf("StatefulSet/%s: %w", ss.Name, err))
			}
		}
	}

	// 3. Services
	services, err := client.Clientset.CoreV1().Services(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, svc := range services.Items {
			if err := client.Clientset.CoreV1().Services(targetNS).Delete(ctx, svc.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("Service/%s", svc.Name))
				fmt.Printf("  ✓ Deleted Service/%s\n", svc.Name)
			} else {
				errors = append(errors, fmt.Errorf("Service/%s: %w", svc.Name, err))
			}
		}
	}

	// 4. Secrets (with kbox labels)
	secrets, err := client.Clientset.CoreV1().Secrets(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, secret := range secrets.Items {
			if err := client.Clientset.CoreV1().Secrets(targetNS).Delete(ctx, secret.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("Secret/%s", secret.Name))
				fmt.Printf("  ✓ Deleted Secret/%s\n", secret.Name)
			} else {
				errors = append(errors, fmt.Errorf("Secret/%s: %w", secret.Name, err))
			}
		}
	}

	// 5. ConfigMaps
	configmaps, err := client.Clientset.CoreV1().ConfigMaps(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, cm := range configmaps.Items {
			if err := client.Clientset.CoreV1().ConfigMaps(targetNS).Delete(ctx, cm.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("ConfigMap/%s", cm.Name))
				fmt.Printf("  ✓ Deleted ConfigMap/%s\n", cm.Name)
			} else {
				errors = append(errors, fmt.Errorf("ConfigMap/%s: %w", cm.Name, err))
			}
		}
	}

	// 6. PVCs (only with --all flag)
	if all {
		pvcs, err := client.Clientset.CoreV1().PersistentVolumeClaims(targetNS).List(ctx, listOpts)
		if err == nil {
			for _, pvc := range pvcs.Items {
				if err := client.Clientset.CoreV1().PersistentVolumeClaims(targetNS).Delete(ctx, pvc.Name, deleteOpts); err == nil {
					deleted = append(deleted, fmt.Sprintf("PersistentVolumeClaim/%s", pvc.Name))
					fmt.Printf("  ✓ Deleted PersistentVolumeClaim/%s\n", pvc.Name)
				} else {
					errors = append(errors, fmt.Errorf("PersistentVolumeClaim/%s: %w", pvc.Name, err))
				}
			}
		}
	}

	// Also delete release history ConfigMap
	releaseHistoryCM := fmt.Sprintf("%s-release-history", appName)
	if err := client.Clientset.CoreV1().ConfigMaps(targetNS).Delete(ctx, releaseHistoryCM, deleteOpts); err == nil {
		deleted = append(deleted, fmt.Sprintf("ConfigMap/%s", releaseHistoryCM))
		fmt.Printf("  ✓ Deleted ConfigMap/%s\n", releaseHistoryCM)
	}

	// Summary
	fmt.Println()
	if len(deleted) == 0 {
		fmt.Printf("No resources found for %q in namespace %q\n", appName, targetNS)
	} else {
		fmt.Printf("Deleted %d resources\n", len(deleted))
	}

	if len(errors) > 0 {
		fmt.Fprintln(os.Stderr, "\nErrors:")
		for _, e := range errors {
			fmt.Fprintf(os.Stderr, "  - %v\n", e)
		}
		return fmt.Errorf("down completed with %d errors", len(errors))
	}

	if !all && len(deleted) > 0 {
		// Check if there are PVCs that weren't deleted
		pvcs, _ := client.Clientset.CoreV1().PersistentVolumeClaims(targetNS).List(context.Background(), listOpts)
		if len(pvcs.Items) > 0 {
			fmt.Printf("\nNote: %d PVC(s) preserved (data intact). Use 'kbox down --all' to delete them.\n", len(pvcs.Items))
		}
	}

	return nil
}

func init() {
	downCmd.Flags().Bool("force", false, "Skip confirmation prompt")
	downCmd.Flags().Bool("all", false, "Also delete PersistentVolumeClaims (data loss!)")
	rootCmd.AddCommand(downCmd)
}
