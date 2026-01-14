package debug

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// AppStatus contains comprehensive status information for an app
type AppStatus struct {
	Name       string
	Namespace  string
	Deployment *DeploymentStatus
	Pods       []PodStatus
	Events     []EventInfo
}

// DeploymentStatus contains deployment-level status
type DeploymentStatus struct {
	Name              string
	Replicas          int32
	ReadyReplicas     int32
	UpdatedReplicas   int32
	AvailableReplicas int32
	Strategy          string
	Image             string
	CreatedAt         time.Time
}

// PodStatus contains pod-level status
type PodStatus struct {
	Name       string
	Phase      string
	Ready      bool
	Restarts   int32
	Age        time.Duration
	IP         string
	Node       string
	Containers []ContainerStatus
}

// ContainerStatus contains container-level status
type ContainerStatus struct {
	Name    string
	Ready   bool
	State   string
	Reason  string
	Message string
}

// EventInfo contains event information
type EventInfo struct {
	Type      string
	Reason    string
	Message   string
	Count     int32
	LastSeen  time.Time
	FirstSeen time.Time
}

// GetAppStatus retrieves comprehensive status for an app
func GetAppStatus(ctx context.Context, client *kubernetes.Clientset, namespace, appName string) (*AppStatus, error) {
	status := &AppStatus{
		Name:      appName,
		Namespace: namespace,
	}

	// Try to find deployment
	deployment, err := findDeployment(ctx, client, namespace, appName)
	if err == nil && deployment != nil {
		status.Deployment = deploymentToStatus(deployment)
	}

	// Find pods
	pods, err := FindPods(ctx, client, namespace, appName)
	if err != nil {
		return nil, err
	}

	// Get detailed pod status
	for _, p := range pods {
		podStatus, err := getPodStatus(ctx, client, namespace, p.Name)
		if err != nil {
			continue
		}
		status.Pods = append(status.Pods, *podStatus)
	}

	// Get recent events
	events, err := getAppEvents(ctx, client, namespace, appName, pods)
	if err == nil {
		status.Events = events
	}

	return status, nil
}

func findDeployment(ctx context.Context, client *kubernetes.Clientset, namespace, appName string) (*appsv1.Deployment, error) {
	// Try direct name match
	dep, err := client.AppsV1().Deployments(namespace).Get(ctx, appName, metav1.GetOptions{})
	if err == nil {
		return dep, nil
	}

	// Try to find by label
	deps, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appName),
	})
	if err != nil {
		return nil, err
	}

	if len(deps.Items) > 0 {
		return &deps.Items[0], nil
	}

	return nil, fmt.Errorf("deployment not found")
}

func deploymentToStatus(dep *appsv1.Deployment) *DeploymentStatus {
	status := &DeploymentStatus{
		Name:              dep.Name,
		Replicas:          *dep.Spec.Replicas,
		ReadyReplicas:     dep.Status.ReadyReplicas,
		UpdatedReplicas:   dep.Status.UpdatedReplicas,
		AvailableReplicas: dep.Status.AvailableReplicas,
		Strategy:          string(dep.Spec.Strategy.Type),
		CreatedAt:         dep.CreationTimestamp.Time,
	}

	// Get image from first container
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		status.Image = dep.Spec.Template.Spec.Containers[0].Image
	}

	return status
}

func getPodStatus(ctx context.Context, client *kubernetes.Clientset, namespace, podName string) (*PodStatus, error) {
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	status := &PodStatus{
		Name:   pod.Name,
		Phase:  string(pod.Status.Phase),
		IP:     pod.Status.PodIP,
		Node:   pod.Spec.NodeName,
		Age:    time.Since(pod.CreationTimestamp.Time),
	}

	// Get container statuses
	for _, cs := range pod.Status.ContainerStatuses {
		cStatus := ContainerStatus{
			Name:  cs.Name,
			Ready: cs.Ready,
		}

		status.Restarts += cs.RestartCount

		if cs.State.Running != nil {
			cStatus.State = "Running"
		} else if cs.State.Waiting != nil {
			cStatus.State = "Waiting"
			cStatus.Reason = cs.State.Waiting.Reason
			cStatus.Message = cs.State.Waiting.Message
		} else if cs.State.Terminated != nil {
			cStatus.State = "Terminated"
			cStatus.Reason = cs.State.Terminated.Reason
			cStatus.Message = cs.State.Terminated.Message
		}

		status.Containers = append(status.Containers, cStatus)
	}

	// Check if pod is ready
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			status.Ready = true
			break
		}
	}

	return status, nil
}

func getAppEvents(ctx context.Context, client *kubernetes.Clientset, namespace, appName string, pods []PodInfo) ([]EventInfo, error) {
	// Get events for the namespace
	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Build set of relevant object names
	relevant := make(map[string]bool)
	relevant[appName] = true // Deployment name
	for _, p := range pods {
		relevant[p.Name] = true
	}

	var result []EventInfo
	for _, e := range events.Items {
		if !relevant[e.InvolvedObject.Name] {
			continue
		}

		// Only show recent events (last hour)
		if time.Since(e.LastTimestamp.Time) > time.Hour {
			continue
		}

		result = append(result, EventInfo{
			Type:      e.Type,
			Reason:    e.Reason,
			Message:   e.Message,
			Count:     e.Count,
			LastSeen:  e.LastTimestamp.Time,
			FirstSeen: e.FirstTimestamp.Time,
		})
	}

	// Sort by last seen, newest first
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastSeen.After(result[j].LastSeen)
	})

	// Limit to 10 most recent
	if len(result) > 10 {
		result = result[:10]
	}

	return result, nil
}

// PrintStatus writes formatted status to output
func PrintStatus(w io.Writer, status *AppStatus) {
	fmt.Fprintf(w, "App: %s (namespace: %s)\n", status.Name, status.Namespace)
	fmt.Fprintln(w)

	// Deployment info
	if status.Deployment != nil {
		d := status.Deployment
		fmt.Fprintln(w, "Deployment:")
		fmt.Fprintf(w, "  Replicas: %d/%d ready, %d updated, %d available\n",
			d.ReadyReplicas, d.Replicas, d.UpdatedReplicas, d.AvailableReplicas)
		fmt.Fprintf(w, "  Strategy: %s\n", d.Strategy)
		fmt.Fprintf(w, "  Image: %s\n", d.Image)
		fmt.Fprintf(w, "  Age: %s\n", formatDuration(time.Since(d.CreatedAt)))
		fmt.Fprintln(w)
	}

	// Pods
	fmt.Fprintf(w, "Pods (%d):\n", len(status.Pods))
	for _, p := range status.Pods {
		readyStr := "ready"
		if !p.Ready {
			readyStr = "not ready"
		}
		fmt.Fprintf(w, "  %s: %s (%s), restarts=%d, age=%s\n",
			p.Name, p.Phase, readyStr, p.Restarts, formatDuration(p.Age))

		// Show container issues
		for _, c := range p.Containers {
			if c.State != "Running" || !c.Ready {
				fmt.Fprintf(w, "    └─ %s: %s", c.Name, c.State)
				if c.Reason != "" {
					fmt.Fprintf(w, " (%s)", c.Reason)
				}
				fmt.Fprintln(w)
			}
		}
	}
	fmt.Fprintln(w)

	// Events
	if len(status.Events) > 0 {
		fmt.Fprintln(w, "Recent Events:")
		for _, e := range status.Events {
			typeColor := "\033[32m" // Green for Normal
			if e.Type == "Warning" {
				typeColor = "\033[33m" // Yellow for Warning
			}
			fmt.Fprintf(w, "  %s%-7s\033[0m %s: %s", typeColor, e.Type, e.Reason, e.Message)
			if e.Count > 1 {
				fmt.Fprintf(w, " (x%d)", e.Count)
			}
			fmt.Fprintf(w, " [%s ago]\n", formatDuration(time.Since(e.LastSeen)))
		}
	} else {
		fmt.Fprintln(w, "Recent Events: none")
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
