package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/bobbyrathoree/kbox/internal/render"
)

const (
	// FieldManager is the field manager name for SSA
	FieldManager = "kbox"

	// DefaultTimeout for operations
	DefaultTimeout = 5 * time.Minute
)

// Engine handles applying Kubernetes resources
type Engine struct {
	client  *kubernetes.Clientset
	out     io.Writer
	timeout time.Duration
}

// NewEngine creates a new apply engine
func NewEngine(client *kubernetes.Clientset, out io.Writer) *Engine {
	return &Engine{
		client:  client,
		out:     out,
		timeout: DefaultTimeout,
	}
}

// SetTimeout sets the timeout for rollout operations
func (e *Engine) SetTimeout(timeout time.Duration) {
	e.timeout = timeout
}

// ApplyResult contains the result of an apply operation
type ApplyResult struct {
	Created []string
	Updated []string
	Errors  []error
}

// Apply applies a bundle to the cluster using Server-Side Apply
func (e *Engine) Apply(ctx context.Context, bundle *render.Bundle) (*ApplyResult, error) {
	result := &ApplyResult{}

	// Stage 0: PersistentVolumeClaims (storage before anything else)
	for _, pvc := range bundle.PersistentVolumeClaims {
		created, err := e.applyPVC(ctx, pvc)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("pvc %s: %w", pvc.Name, err))
			continue
		}
		if created {
			result.Created = append(result.Created, fmt.Sprintf("PersistentVolumeClaim/%s", pvc.Name))
		} else {
			result.Updated = append(result.Updated, fmt.Sprintf("PersistentVolumeClaim/%s", pvc.Name))
		}
		fmt.Fprintf(e.out, "  ✓ PersistentVolumeClaim/%s\n", pvc.Name)
	}

	// Stage 1: ConfigMaps and Secrets (config first)
	for _, cm := range bundle.ConfigMaps {
		created, err := e.applyConfigMap(ctx, cm)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("configmap %s: %w", cm.Name, err))
			continue
		}
		if created {
			result.Created = append(result.Created, fmt.Sprintf("ConfigMap/%s", cm.Name))
		} else {
			result.Updated = append(result.Updated, fmt.Sprintf("ConfigMap/%s", cm.Name))
		}
		fmt.Fprintf(e.out, "  ✓ ConfigMap/%s\n", cm.Name)
	}

	for _, secret := range bundle.Secrets {
		created, err := e.applySecret(ctx, secret)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("secret %s: %w", secret.Name, err))
			continue
		}
		if created {
			result.Created = append(result.Created, fmt.Sprintf("Secret/%s", secret.Name))
		} else {
			result.Updated = append(result.Updated, fmt.Sprintf("Secret/%s", secret.Name))
		}
		fmt.Fprintf(e.out, "  ✓ Secret/%s\n", secret.Name)
	}

	// Stage 2: Services
	for _, svc := range bundle.Services {
		created, err := e.applyService(ctx, svc)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("service %s: %w", svc.Name, err))
			continue
		}
		if created {
			result.Created = append(result.Created, fmt.Sprintf("Service/%s", svc.Name))
		} else {
			result.Updated = append(result.Updated, fmt.Sprintf("Service/%s", svc.Name))
		}
		fmt.Fprintf(e.out, "  ✓ Service/%s\n", svc.Name)
	}

	// Stage 2.5: StatefulSets (databases/dependencies must be ready before app Deployment)
	for _, ss := range bundle.StatefulSets {
		created, err := e.applyStatefulSet(ctx, ss)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("statefulset %s: %w", ss.Name, err))
			continue
		}
		if created {
			result.Created = append(result.Created, fmt.Sprintf("StatefulSet/%s", ss.Name))
		} else {
			result.Updated = append(result.Updated, fmt.Sprintf("StatefulSet/%s", ss.Name))
		}
		fmt.Fprintf(e.out, "  ✓ StatefulSet/%s\n", ss.Name)
	}

	// Stage 3: Deployment
	if bundle.Deployment != nil {
		created, err := e.applyDeployment(ctx, bundle.Deployment)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("deployment %s: %w", bundle.Deployment.Name, err))
		} else {
			if created {
				result.Created = append(result.Created, fmt.Sprintf("Deployment/%s", bundle.Deployment.Name))
			} else {
				result.Updated = append(result.Updated, fmt.Sprintf("Deployment/%s", bundle.Deployment.Name))
			}
			fmt.Fprintf(e.out, "  ✓ Deployment/%s\n", bundle.Deployment.Name)
		}
	}

	// Stage 4: Ingresses
	for _, ing := range bundle.Ingresses {
		created, err := e.applyIngress(ctx, ing)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("ingress %s: %w", ing.Name, err))
			continue
		}
		if created {
			result.Created = append(result.Created, fmt.Sprintf("Ingress/%s", ing.Name))
		} else {
			result.Updated = append(result.Updated, fmt.Sprintf("Ingress/%s", ing.Name))
		}
		fmt.Fprintf(e.out, "  ✓ Ingress/%s\n", ing.Name)
	}

	// Stage 5: Jobs
	for _, job := range bundle.Jobs {
		created, err := e.applyJob(ctx, job)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("job %s: %w", job.Name, err))
			continue
		}
		if created {
			result.Created = append(result.Created, fmt.Sprintf("Job/%s", job.Name))
		} else {
			result.Updated = append(result.Updated, fmt.Sprintf("Job/%s", job.Name))
		}
		fmt.Fprintf(e.out, "  ✓ Job/%s\n", job.Name)
	}

	// Stage 6: CronJobs
	for _, cronJob := range bundle.CronJobs {
		created, err := e.applyCronJob(ctx, cronJob)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("cronjob %s: %w", cronJob.Name, err))
			continue
		}
		if created {
			result.Created = append(result.Created, fmt.Sprintf("CronJob/%s", cronJob.Name))
		} else {
			result.Updated = append(result.Updated, fmt.Sprintf("CronJob/%s", cronJob.Name))
		}
		fmt.Fprintf(e.out, "  ✓ CronJob/%s\n", cronJob.Name)
	}

	return result, nil
}

// WaitForRollout waits for a deployment to complete its rollout
func (e *Engine) WaitForRollout(ctx context.Context, namespace, name string) error {
	fmt.Fprintf(e.out, "  ⠋ Waiting for rollout...")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(e.timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for rollout\n  → Run 'kbox logs' to check for errors\n  → Run 'kbox status' to see pod state")
		case <-ticker.C:
			dep, err := e.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				continue
			}

			// Check for pod failures (crashloops, image pull errors)
			pods, err := e.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", name),
			})
			if err == nil {
				for _, pod := range pods.Items {
					for _, cs := range pod.Status.ContainerStatuses {
						if cs.State.Waiting != nil {
							reason := cs.State.Waiting.Reason
							if reason == "CrashLoopBackOff" ||
								reason == "ImagePullBackOff" ||
								reason == "ErrImagePull" {
								fmt.Fprintf(e.out, "\r")
								return fmt.Errorf("pod %s: %s\n  → Run 'kbox logs' to diagnose\n  → Run 'kbox status' to see events",
									pod.Name, reason)
							}
						}
					}
				}
			}

			// Check rollout status
			if dep.Status.ObservedGeneration >= dep.Generation {
				if dep.Status.UpdatedReplicas == *dep.Spec.Replicas &&
					dep.Status.ReadyReplicas == *dep.Spec.Replicas &&
					dep.Status.AvailableReplicas == *dep.Spec.Replicas {
					fmt.Fprintf(e.out, "\r  ✓ Rollout complete (%d/%d pods ready)\n",
						dep.Status.ReadyReplicas, *dep.Spec.Replicas)
					return nil
				}
			}

			// Update progress
			fmt.Fprintf(e.out, "\r  ⠋ Waiting for rollout... (%d/%d pods ready)",
				dep.Status.ReadyReplicas, *dep.Spec.Replicas)
		}
	}
}

func (e *Engine) applyPVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	return e.applyObject(ctx, pvc, "persistentvolumeclaims", pvc.Namespace, pvc.Name)
}

func (e *Engine) applyConfigMap(ctx context.Context, cm *corev1.ConfigMap) (bool, error) {
	return e.applyObject(ctx, cm, "configmaps", cm.Namespace, cm.Name)
}

func (e *Engine) applySecret(ctx context.Context, secret *corev1.Secret) (bool, error) {
	return e.applyObject(ctx, secret, "secrets", secret.Namespace, secret.Name)
}

func (e *Engine) applyService(ctx context.Context, svc *corev1.Service) (bool, error) {
	return e.applyObject(ctx, svc, "services", svc.Namespace, svc.Name)
}

func (e *Engine) applyDeployment(ctx context.Context, dep *appsv1.Deployment) (bool, error) {
	return e.applyObject(ctx, dep, "deployments", dep.Namespace, dep.Name)
}

func (e *Engine) applyStatefulSet(ctx context.Context, ss *appsv1.StatefulSet) (bool, error) {
	return e.applyObject(ctx, ss, "statefulsets", ss.Namespace, ss.Name)
}

func (e *Engine) applyIngress(ctx context.Context, ing *networkingv1.Ingress) (bool, error) {
	return e.applyObject(ctx, ing, "ingresses", ing.Namespace, ing.Name)
}

func (e *Engine) applyJob(ctx context.Context, job *batchv1.Job) (bool, error) {
	return e.applyObject(ctx, job, "jobs", job.Namespace, job.Name)
}

func (e *Engine) applyCronJob(ctx context.Context, cronJob *batchv1.CronJob) (bool, error) {
	return e.applyObject(ctx, cronJob, "cronjobs", cronJob.Namespace, cronJob.Name)
}

func (e *Engine) applyObject(ctx context.Context, obj runtime.Object, resource, namespace, name string) (bool, error) {
	// Convert object to JSON for SSA patch
	data, err := json.Marshal(obj)
	if err != nil {
		return false, fmt.Errorf("failed to marshal object: %w", err)
	}

	// Check if object exists
	var exists bool
	switch resource {
	case "persistentvolumeclaims":
		_, err = e.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	case "configmaps":
		_, err = e.client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	case "secrets":
		_, err = e.client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	case "services":
		_, err = e.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	case "deployments":
		_, err = e.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	case "statefulsets":
		_, err = e.client.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	case "ingresses":
		_, err = e.client.NetworkingV1().Ingresses(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	case "jobs":
		_, err = e.client.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	case "cronjobs":
		_, err = e.client.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
		exists = err == nil
	}

	// Apply using SSA
	patchOpts := metav1.PatchOptions{
		FieldManager: FieldManager,
	}

	switch resource {
	case "persistentvolumeclaims":
		_, err = e.client.CoreV1().PersistentVolumeClaims(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	case "configmaps":
		_, err = e.client.CoreV1().ConfigMaps(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	case "secrets":
		_, err = e.client.CoreV1().Secrets(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	case "services":
		_, err = e.client.CoreV1().Services(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	case "deployments":
		_, err = e.client.AppsV1().Deployments(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	case "statefulsets":
		_, err = e.client.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	case "ingresses":
		_, err = e.client.NetworkingV1().Ingresses(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	case "jobs":
		_, err = e.client.BatchV1().Jobs(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	case "cronjobs":
		_, err = e.client.BatchV1().CronJobs(namespace).Patch(ctx, name, types.ApplyPatchType, data, patchOpts)
	default:
		return false, fmt.Errorf("unknown resource type: %s", resource)
	}

	if err != nil {
		if errors.IsNotFound(err) {
			return false, fmt.Errorf("namespace %q does not exist\n  → Create it: kubectl create namespace %s", namespace, namespace)
		}
		if errors.IsForbidden(err) {
			return false, fmt.Errorf("permission denied: %w\n  → Check your RBAC permissions for the target namespace", err)
		}
		return false, err
	}

	return !exists, nil
}
