package apply

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bobbyrathoree/kbox/internal/render"
)

// PruneOptions configures pruning behavior
type PruneOptions struct {
	DryRun bool
}

// PruneResult contains the result of a prune operation
type PruneResult struct {
	Deleted []string
	Errors  []error
}

// Prune removes resources with label app=<appName> that aren't in bundle.
// This prevents orphaned resources when config changes remove resources.
func (e *Engine) Prune(ctx context.Context, namespace, appName string, bundle *render.Bundle, opts PruneOptions) (*PruneResult, error) {
	result := &PruneResult{}

	// Build set of resource identifiers from bundle
	bundleResources := make(map[string]bool)
	for _, cm := range bundle.ConfigMaps {
		bundleResources[fmt.Sprintf("ConfigMap/%s", cm.Name)] = true
	}
	for _, secret := range bundle.Secrets {
		bundleResources[fmt.Sprintf("Secret/%s", secret.Name)] = true
	}
	for _, svc := range bundle.Services {
		bundleResources[fmt.Sprintf("Service/%s", svc.Name)] = true
	}
	for _, ss := range bundle.StatefulSets {
		bundleResources[fmt.Sprintf("StatefulSet/%s", ss.Name)] = true
	}
	for _, dep := range bundle.Deployments {
		bundleResources[fmt.Sprintf("Deployment/%s", dep.Name)] = true
	}
	if bundle.Deployment != nil {
		bundleResources[fmt.Sprintf("Deployment/%s", bundle.Deployment.Name)] = true
	}
	for _, job := range bundle.Jobs {
		bundleResources[fmt.Sprintf("Job/%s", job.Name)] = true
	}
	for _, cronJob := range bundle.CronJobs {
		bundleResources[fmt.Sprintf("CronJob/%s", cronJob.Name)] = true
	}
	for _, ing := range bundle.Ingresses {
		bundleResources[fmt.Sprintf("Ingress/%s", ing.Name)] = true
	}
	for _, np := range bundle.NetworkPolicies {
		bundleResources[fmt.Sprintf("NetworkPolicy/%s", np.Name)] = true
	}
	if bundle.HPA != nil {
		bundleResources[fmt.Sprintf("HorizontalPodAutoscaler/%s", bundle.HPA.Name)] = true
	}
	if bundle.PDB != nil {
		bundleResources[fmt.Sprintf("PodDisruptionBudget/%s", bundle.PDB.Name)] = true
	}
	if bundle.ServiceAccount != nil {
		bundleResources[fmt.Sprintf("ServiceAccount/%s", bundle.ServiceAccount.Name)] = true
	}
	for _, pvc := range bundle.PersistentVolumeClaims {
		bundleResources[fmt.Sprintf("PersistentVolumeClaim/%s", pvc.Name)] = true
	}

	labelSelector := fmt.Sprintf("app=%s", appName)
	deletePolicy := metav1.DeletePropagationForeground

	// Prune ConfigMaps
	cms, err := e.client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, cm := range cms.Items {
			key := fmt.Sprintf("ConfigMap/%s", cm.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.CoreV1().ConfigMaps(namespace).Delete(ctx, cm.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune Secrets
	secrets, err := e.client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, secret := range secrets.Items {
			key := fmt.Sprintf("Secret/%s", secret.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.CoreV1().Secrets(namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune Services
	svcs, err := e.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, svc := range svcs.Items {
			key := fmt.Sprintf("Service/%s", svc.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.CoreV1().Services(namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune Deployments
	deps, err := e.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, dep := range deps.Items {
			key := fmt.Sprintf("Deployment/%s", dep.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.AppsV1().Deployments(namespace).Delete(ctx, dep.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune StatefulSets
	ssets, err := e.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, ss := range ssets.Items {
			key := fmt.Sprintf("StatefulSet/%s", ss.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.AppsV1().StatefulSets(namespace).Delete(ctx, ss.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune Ingresses
	ings, err := e.client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, ing := range ings.Items {
			key := fmt.Sprintf("Ingress/%s", ing.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.NetworkingV1().Ingresses(namespace).Delete(ctx, ing.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune NetworkPolicies
	nps, err := e.client.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, np := range nps.Items {
			key := fmt.Sprintf("NetworkPolicy/%s", np.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, np.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune HPA
	hpas, err := e.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, hpa := range hpas.Items {
			key := fmt.Sprintf("HorizontalPodAutoscaler/%s", hpa.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).Delete(ctx, hpa.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune PDB
	pdbs, err := e.client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, pdb := range pdbs.Items {
			key := fmt.Sprintf("PodDisruptionBudget/%s", pdb.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.PolicyV1().PodDisruptionBudgets(namespace).Delete(ctx, pdb.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune Jobs
	jobs, err := e.client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, job := range jobs.Items {
			key := fmt.Sprintf("Job/%s", job.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.BatchV1().Jobs(namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	// Prune CronJobs
	cronJobs, err := e.client.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		for _, cronJob := range cronJobs.Items {
			key := fmt.Sprintf("CronJob/%s", cronJob.Name)
			if !bundleResources[key] {
				if !opts.DryRun {
					if err := e.client.BatchV1().CronJobs(namespace).Delete(ctx, cronJob.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePolicy,
					}); err != nil {
						result.Errors = append(result.Errors, fmt.Errorf("failed to delete %s: %w", key, err))
						continue
					}
				}
				result.Deleted = append(result.Deleted, key)
				fmt.Fprintf(e.out, "  ✓ Pruned %s\n", key)
			}
		}
	}

	return result, nil
}
