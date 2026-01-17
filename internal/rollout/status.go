package rollout

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Status represents the current rollout status
type Status struct {
	// Deployment name
	Name string

	// Namespace
	Namespace string

	// Strategy type (RollingUpdate, Recreate)
	Strategy string

	// Current status (Progressing, Complete, Failed, Paused)
	State string

	// Image being deployed
	Image string

	// Total desired replicas
	DesiredReplicas int32

	// Number of updated replicas
	UpdatedReplicas int32

	// Number of ready replicas
	ReadyReplicas int32

	// Number of available replicas
	AvailableReplicas int32

	// When the rollout started
	StartTime *time.Time

	// Pods associated with the deployment
	Pods []PodStatus

	// Whether deployment is paused
	Paused bool

	// Message from conditions
	Message string
}

// PodStatus represents a pod's status
type PodStatus struct {
	Name   string
	Status string
	Image  string
	IsNew  bool
	Age    time.Duration
}

// GetStatus retrieves the current rollout status for a deployment
func GetStatus(ctx context.Context, client *kubernetes.Clientset, namespace, name string) (*Status, error) {
	// Get the deployment
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("deployment %q not found: %w", name, err)
	}

	status := &Status{
		Name:              deployment.Name,
		Namespace:         deployment.Namespace,
		Strategy:          string(deployment.Spec.Strategy.Type),
		DesiredReplicas:   *deployment.Spec.Replicas,
		UpdatedReplicas:   deployment.Status.UpdatedReplicas,
		ReadyReplicas:     deployment.Status.ReadyReplicas,
		AvailableReplicas: deployment.Status.AvailableReplicas,
		Paused:            deployment.Spec.Paused,
	}

	// Get image from container spec
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		status.Image = deployment.Spec.Template.Spec.Containers[0].Image
	}

	// Determine state from conditions
	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing {
			if cond.Status == corev1.ConditionTrue {
				if cond.Reason == "NewReplicaSetAvailable" {
					status.State = "Complete"
				} else {
					status.State = "Progressing"
				}
			} else if cond.Reason == "ProgressDeadlineExceeded" {
				status.State = "Failed"
			}
			status.Message = cond.Message
			if cond.LastTransitionTime.Time.After(time.Time{}) {
				t := cond.LastTransitionTime.Time
				status.StartTime = &t
			}
		}
	}

	if status.Paused {
		status.State = "Paused"
	}

	if status.State == "" {
		if status.ReadyReplicas == status.DesiredReplicas {
			status.State = "Complete"
		} else {
			status.State = "Progressing"
		}
	}

	// Get pods
	labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err == nil {
		currentRS := getCurrentReplicaSet(ctx, client, deployment)
		for _, pod := range pods.Items {
			isNew := false
			if currentRS != "" {
				// Check if pod belongs to current ReplicaSet
				for _, ref := range pod.OwnerReferences {
					if ref.Kind == "ReplicaSet" && ref.Name == currentRS {
						isNew = true
						break
					}
				}
			}

			podStatus := PodStatus{
				Name:  pod.Name,
				IsNew: isNew,
				Age:   time.Since(pod.CreationTimestamp.Time),
			}

			// Get pod status
			switch pod.Status.Phase {
			case corev1.PodRunning:
				podStatus.Status = "Running"
			case corev1.PodPending:
				podStatus.Status = "Pending"
			case corev1.PodFailed:
				podStatus.Status = "Failed"
			case corev1.PodSucceeded:
				podStatus.Status = "Completed"
			default:
				podStatus.Status = string(pod.Status.Phase)
			}

			// Check for container issues
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					podStatus.Status = cs.State.Waiting.Reason
				}
			}

			// Get image
			if len(pod.Spec.Containers) > 0 {
				podStatus.Image = pod.Spec.Containers[0].Image
			}

			status.Pods = append(status.Pods, podStatus)
		}
	}

	return status, nil
}

// getCurrentReplicaSet finds the current (newest) ReplicaSet for a deployment
func getCurrentReplicaSet(ctx context.Context, client *kubernetes.Clientset, deployment *appsv1.Deployment) string {
	rsList, err := client.AppsV1().ReplicaSets(deployment.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
	})
	if err != nil {
		return ""
	}

	var newest *appsv1.ReplicaSet
	for i := range rsList.Items {
		rs := &rsList.Items[i]
		// Check if this RS belongs to our deployment
		for _, ref := range rs.OwnerReferences {
			if ref.Kind == "Deployment" && ref.Name == deployment.Name {
				if newest == nil || rs.CreationTimestamp.After(newest.CreationTimestamp.Time) {
					newest = rs
				}
			}
		}
	}

	if newest != nil {
		return newest.Name
	}
	return ""
}

// PrintStatus prints the status in a formatted way
func PrintStatus(w io.Writer, status *Status) {
	fmt.Fprintf(w, "Deployment: %s\n", status.Name)
	fmt.Fprintf(w, "Namespace:  %s\n", status.Namespace)
	fmt.Fprintf(w, "Strategy:   %s\n", status.Strategy)
	fmt.Fprintln(w)

	// State with color hint
	stateColor := ""
	switch status.State {
	case "Complete":
		stateColor = "Complete"
	case "Progressing":
		stateColor = "Progressing"
	case "Failed":
		stateColor = "FAILED"
	case "Paused":
		stateColor = "Paused"
	}
	fmt.Fprintf(w, "Status:     %s\n", stateColor)
	fmt.Fprintf(w, "Progress:   %d/%d pods updated\n", status.UpdatedReplicas, status.DesiredReplicas)
	fmt.Fprintf(w, "Image:      %s\n", status.Image)

	if status.StartTime != nil {
		fmt.Fprintf(w, "Started:    %s ago\n", formatDuration(time.Since(*status.StartTime)))
	}

	// Print pods
	if len(status.Pods) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Pods:")
		fmt.Fprintf(w, "  %-35s %-15s %s\n", "NAME", "STATUS", "IMAGE")
		for _, pod := range status.Pods {
			newMarker := ""
			if pod.IsNew {
				newMarker = " (new)"
			} else {
				newMarker = " (old)"
			}
			// Truncate pod name and image if too long
			name := pod.Name
			if len(name) > 33 {
				name = name[:30] + "..."
			}
			image := pod.Image
			// Just show tag portion
			if idx := strings.LastIndex(image, ":"); idx > 0 {
				image = "..." + image[idx:]
			}
			fmt.Fprintf(w, "  %-35s %-15s %s%s\n", name, pod.Status, image, newMarker)
		}
	}
}

// PrintProgressBar prints a simple progress bar
func PrintProgressBar(w io.Writer, current, total int32, width int) {
	if total == 0 {
		total = 1
	}
	percent := float64(current) / float64(total)
	filled := int(percent * float64(width))

	bar := strings.Repeat("=", filled)
	if filled < width {
		bar += ">"
		bar += strings.Repeat(" ", width-filled-1)
	}

	fmt.Fprintf(w, "  Progress: [%s] %d/%d pods (%d%%)\n", bar, current, total, int(percent*100))
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
