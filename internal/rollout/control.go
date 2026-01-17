package rollout

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Pause pauses a deployment rollout
func Pause(ctx context.Context, client *kubernetes.Clientset, namespace, name string) error {
	// Check if already paused
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("deployment %q not found: %w", name, err)
	}

	if deployment.Spec.Paused {
		return fmt.Errorf("deployment %q is already paused", name)
	}

	// Patch to pause
	patch := []byte(`{"spec":{"paused":true}}`)
	_, err = client.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to pause deployment: %w", err)
	}

	return nil
}

// Resume resumes a paused deployment rollout
func Resume(ctx context.Context, client *kubernetes.Clientset, namespace, name string) error {
	// Check if paused
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("deployment %q not found: %w", name, err)
	}

	if !deployment.Spec.Paused {
		return fmt.Errorf("deployment %q is not paused", name)
	}

	// Patch to resume
	patch := []byte(`{"spec":{"paused":false}}`)
	_, err = client.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to resume deployment: %w", err)
	}

	return nil
}

// Undo rolls back a deployment to the previous revision
func Undo(ctx context.Context, client *kubernetes.Clientset, namespace, name string) error {
	// Get the deployment
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("deployment %q not found: %w", name, err)
	}

	// Find the previous ReplicaSet
	rsList, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
	})
	if err != nil {
		return fmt.Errorf("failed to list ReplicaSets: %w", err)
	}

	// Find ReplicaSets owned by this deployment, sorted by revision
	type rsInfo struct {
		name     string
		revision int64
		image    string
	}
	var replicaSets []rsInfo

	for _, rs := range rsList.Items {
		// Check if owned by this deployment
		owned := false
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" && ref.Name == deployment.Name {
				owned = true
				break
			}
		}
		if !owned {
			continue
		}

		// Get revision from annotation
		revStr := rs.Annotations["deployment.kubernetes.io/revision"]
		var rev int64
		fmt.Sscanf(revStr, "%d", &rev)

		image := ""
		if len(rs.Spec.Template.Spec.Containers) > 0 {
			image = rs.Spec.Template.Spec.Containers[0].Image
		}

		replicaSets = append(replicaSets, rsInfo{
			name:     rs.Name,
			revision: rev,
			image:    image,
		})
	}

	if len(replicaSets) < 2 {
		return fmt.Errorf("no previous revision found to rollback to")
	}

	// Sort by revision descending
	for i := 0; i < len(replicaSets)-1; i++ {
		for j := i + 1; j < len(replicaSets); j++ {
			if replicaSets[j].revision > replicaSets[i].revision {
				replicaSets[i], replicaSets[j] = replicaSets[j], replicaSets[i]
			}
		}
	}

	// Get the second-newest (previous) ReplicaSet
	previousRS := replicaSets[1]

	// The rollback is done by patching the deployment with the previous pod template
	// Get the previous RS's pod template
	rs, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, previousRS.name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get previous ReplicaSet: %w", err)
	}

	// Update deployment's pod template to match the previous RS
	deployment.Spec.Template = rs.Spec.Template

	_, err = client.AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to rollback deployment: %w", err)
	}

	return nil
}

// GetPreviousImage returns the image from the previous revision
func GetPreviousImage(ctx context.Context, client *kubernetes.Clientset, namespace, name string) (string, error) {
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	rsList, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
	})
	if err != nil {
		return "", err
	}

	// Find second-newest RS owned by this deployment
	type rsInfo struct {
		revision int64
		image    string
	}
	var replicaSets []rsInfo

	for _, rs := range rsList.Items {
		owned := false
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" && ref.Name == deployment.Name {
				owned = true
				break
			}
		}
		if !owned {
			continue
		}

		revStr := rs.Annotations["deployment.kubernetes.io/revision"]
		var rev int64
		fmt.Sscanf(revStr, "%d", &rev)

		image := ""
		if len(rs.Spec.Template.Spec.Containers) > 0 {
			image = rs.Spec.Template.Spec.Containers[0].Image
		}

		replicaSets = append(replicaSets, rsInfo{revision: rev, image: image})
	}

	if len(replicaSets) < 2 {
		return "", fmt.Errorf("no previous revision")
	}

	// Sort by revision descending
	for i := 0; i < len(replicaSets)-1; i++ {
		for j := i + 1; j < len(replicaSets); j++ {
			if replicaSets[j].revision > replicaSets[i].revision {
				replicaSets[i], replicaSets[j] = replicaSets[j], replicaSets[i]
			}
		}
	}

	return replicaSets[1].image, nil
}
