package cli

import (
	"bufio"
	"context"
	"encoding/json"
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

This removes: Deployment, Jobs, CronJobs, StatefulSets, Service, Ingress, ConfigMaps, Secrets.
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
	outputFormat := GetOutputFormat(cmd)

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

	// Only print deletion messages if NOT in JSON mode
	shouldPrint := outputFormat != "json"

	var deleted []string
	var errors []error

	// Delete in reverse dependency order

	// 1. Deployments (app first)
	deps, err := client.Clientset.AppsV1().Deployments(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, dep := range deps.Items {
			if err := client.Clientset.AppsV1().Deployments(targetNS).Delete(ctx, dep.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("Deployment/%s", dep.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted Deployment/%s\n", dep.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("Deployment/%s: %w", dep.Name, err))
			}
		}
	}

	// 2. Jobs (one-off jobs)
	jobs, err := client.Clientset.BatchV1().Jobs(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, job := range jobs.Items {
			if err := client.Clientset.BatchV1().Jobs(targetNS).Delete(ctx, job.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("Job/%s", job.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted Job/%s\n", job.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("Job/%s: %w", job.Name, err))
			}
		}
	}

	// 3. CronJobs (scheduled jobs)
	cronjobs, err := client.Clientset.BatchV1().CronJobs(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, cj := range cronjobs.Items {
			if err := client.Clientset.BatchV1().CronJobs(targetNS).Delete(ctx, cj.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("CronJob/%s", cj.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted CronJob/%s\n", cj.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("CronJob/%s: %w", cj.Name, err))
			}
		}
	}

	// 4. StatefulSets (dependencies like postgres/redis)
	statefulsets, err := client.Clientset.AppsV1().StatefulSets(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, ss := range statefulsets.Items {
			if err := client.Clientset.AppsV1().StatefulSets(targetNS).Delete(ctx, ss.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("StatefulSet/%s", ss.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted StatefulSet/%s\n", ss.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("StatefulSet/%s: %w", ss.Name, err))
			}
		}
	}

	// 5. Services
	services, err := client.Clientset.CoreV1().Services(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, svc := range services.Items {
			if err := client.Clientset.CoreV1().Services(targetNS).Delete(ctx, svc.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("Service/%s", svc.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted Service/%s\n", svc.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("Service/%s: %w", svc.Name, err))
			}
		}
	}

	// 6. Ingresses
	ingresses, err := client.Clientset.NetworkingV1().Ingresses(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, ing := range ingresses.Items {
			if err := client.Clientset.NetworkingV1().Ingresses(targetNS).Delete(ctx, ing.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("Ingress/%s", ing.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted Ingress/%s\n", ing.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("Ingress/%s: %w", ing.Name, err))
			}
		}
	}

	// 7. ConfigMaps
	configmaps, err := client.Clientset.CoreV1().ConfigMaps(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, cm := range configmaps.Items {
			if err := client.Clientset.CoreV1().ConfigMaps(targetNS).Delete(ctx, cm.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("ConfigMap/%s", cm.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted ConfigMap/%s\n", cm.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("ConfigMap/%s: %w", cm.Name, err))
			}
		}
	}

	// 8. Secrets (with kbox labels)
	secrets, err := client.Clientset.CoreV1().Secrets(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, secret := range secrets.Items {
			if err := client.Clientset.CoreV1().Secrets(targetNS).Delete(ctx, secret.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("Secret/%s", secret.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted Secret/%s\n", secret.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("Secret/%s: %w", secret.Name, err))
			}
		}
	}

	// 9. PVCs (only with --all flag, data last)
	if all {
		pvcs, err := client.Clientset.CoreV1().PersistentVolumeClaims(targetNS).List(ctx, listOpts)
		if err == nil {
			for _, pvc := range pvcs.Items {
				if err := client.Clientset.CoreV1().PersistentVolumeClaims(targetNS).Delete(ctx, pvc.Name, deleteOpts); err == nil {
					deleted = append(deleted, fmt.Sprintf("PersistentVolumeClaim/%s", pvc.Name))
					if shouldPrint {
						fmt.Printf("  ✓ Deleted PersistentVolumeClaim/%s\n", pvc.Name)
					}
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
		if shouldPrint {
			fmt.Printf("  ✓ Deleted ConfigMap/%s\n", releaseHistoryCM)
		}
	}

	// Phase 2: Clean up dependency resources (postgres/redis StatefulSets, Services, Secrets)
	// Dependencies use the kbox.dev/app label instead of the app label
	depSelector := fmt.Sprintf("kbox.dev/app=%s", appName)
	depListOpts := metav1.ListOptions{LabelSelector: depSelector}

	// Track already deleted resources to avoid duplicates
	deletedSet := make(map[string]bool)
	for _, d := range deleted {
		deletedSet[d] = true
	}

	// Delete dependency StatefulSets
	depStatefulsets, err := client.Clientset.AppsV1().StatefulSets(targetNS).List(ctx, depListOpts)
	if err == nil {
		for _, ss := range depStatefulsets.Items {
			resourceKey := fmt.Sprintf("StatefulSet/%s", ss.Name)
			if deletedSet[resourceKey] {
				continue // Already deleted in main app cleanup
			}
			if err := client.Clientset.AppsV1().StatefulSets(targetNS).Delete(ctx, ss.Name, deleteOpts); err == nil {
				deleted = append(deleted, resourceKey)
				deletedSet[resourceKey] = true
				if shouldPrint {
					fmt.Printf("  ✓ Deleted StatefulSet/%s\n", ss.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("StatefulSet/%s: %w", ss.Name, err))
			}
		}
	}

	// Delete dependency Services
	depServices, err := client.Clientset.CoreV1().Services(targetNS).List(ctx, depListOpts)
	if err == nil {
		for _, svc := range depServices.Items {
			resourceKey := fmt.Sprintf("Service/%s", svc.Name)
			if deletedSet[resourceKey] {
				continue // Already deleted in main app cleanup
			}
			if err := client.Clientset.CoreV1().Services(targetNS).Delete(ctx, svc.Name, deleteOpts); err == nil {
				deleted = append(deleted, resourceKey)
				deletedSet[resourceKey] = true
				if shouldPrint {
					fmt.Printf("  ✓ Deleted Service/%s\n", svc.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("Service/%s: %w", svc.Name, err))
			}
		}
	}

	// Delete dependency Secrets
	depSecrets, err := client.Clientset.CoreV1().Secrets(targetNS).List(ctx, depListOpts)
	if err == nil {
		for _, secret := range depSecrets.Items {
			resourceKey := fmt.Sprintf("Secret/%s", secret.Name)
			if deletedSet[resourceKey] {
				continue // Already deleted in main app cleanup
			}
			if err := client.Clientset.CoreV1().Secrets(targetNS).Delete(ctx, secret.Name, deleteOpts); err == nil {
				deleted = append(deleted, resourceKey)
				deletedSet[resourceKey] = true
				if shouldPrint {
					fmt.Printf("  ✓ Deleted Secret/%s\n", secret.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("Secret/%s: %w", secret.Name, err))
			}
		}
	}

	// 10. Delete ServiceAccounts (skip default)
	serviceAccounts, err := client.Clientset.CoreV1().ServiceAccounts(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, sa := range serviceAccounts.Items {
			if sa.Name == "default" {
				continue // Don't delete default SA
			}
			if err := client.Clientset.CoreV1().ServiceAccounts(targetNS).Delete(ctx, sa.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("ServiceAccount/%s", sa.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted ServiceAccount/%s\n", sa.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("ServiceAccount/%s: %w", sa.Name, err))
			}
		}
	}

	// 11. Delete NetworkPolicies
	networkPolicies, err := client.Clientset.NetworkingV1().NetworkPolicies(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, np := range networkPolicies.Items {
			if err := client.Clientset.NetworkingV1().NetworkPolicies(targetNS).Delete(ctx, np.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("NetworkPolicy/%s", np.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted NetworkPolicy/%s\n", np.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("NetworkPolicy/%s: %w", np.Name, err))
			}
		}
	}

	// 12. Delete HPAs
	hpas, err := client.Clientset.AutoscalingV2().HorizontalPodAutoscalers(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, hpa := range hpas.Items {
			if err := client.Clientset.AutoscalingV2().HorizontalPodAutoscalers(targetNS).Delete(ctx, hpa.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("HorizontalPodAutoscaler/%s", hpa.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted HorizontalPodAutoscaler/%s\n", hpa.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("HorizontalPodAutoscaler/%s: %w", hpa.Name, err))
			}
		}
	}

	// 13. Delete PDBs
	pdbs, err := client.Clientset.PolicyV1().PodDisruptionBudgets(targetNS).List(ctx, listOpts)
	if err == nil {
		for _, pdb := range pdbs.Items {
			if err := client.Clientset.PolicyV1().PodDisruptionBudgets(targetNS).Delete(ctx, pdb.Name, deleteOpts); err == nil {
				deleted = append(deleted, fmt.Sprintf("PodDisruptionBudget/%s", pdb.Name))
				if shouldPrint {
					fmt.Printf("  ✓ Deleted PodDisruptionBudget/%s\n", pdb.Name)
				}
			} else {
				errors = append(errors, fmt.Errorf("PodDisruptionBudget/%s: %w", pdb.Name, err))
			}
		}
	}

	// JSON output
	if outputFormat == "json" {
		errorStrings := make([]string, len(errors))
		for i, e := range errors {
			errorStrings[i] = e.Error()
		}
		result := map[string]interface{}{
			"success": len(errors) == 0,
			"deleted": deleted,
		}
		if len(errors) > 0 {
			result["errors"] = errorStrings
		}
		return json.NewEncoder(os.Stdout).Encode(result)
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
