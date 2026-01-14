package debug

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodInfo contains information about a pod for debugging
type PodInfo struct {
	Name          string
	Namespace     string
	ContainerName string
	Ready         bool
	Restarts      int32
	Status        string
}

// FindPods finds pods matching an app name in a namespace
// It tries multiple strategies:
// 1. Direct pod name match
// 2. Deployment/Service name via app label
// 3. kbox.dev/app label
func FindPods(ctx context.Context, client *kubernetes.Clientset, namespace, appName string) ([]PodInfo, error) {
	// Try to find pods with various label selectors
	selectors := []string{
		fmt.Sprintf("app=%s", appName),
		fmt.Sprintf("app.kubernetes.io/name=%s", appName),
		fmt.Sprintf("kbox.dev/app=%s", appName),
	}

	var allPods []PodInfo
	seen := make(map[string]bool)

	for _, selector := range selectors {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			continue
		}

		for _, pod := range pods.Items {
			if seen[pod.Name] {
				continue
			}
			seen[pod.Name] = true

			info := podToPodInfo(&pod)
			allPods = append(allPods, info)
		}
	}

	// If no pods found via labels, try direct name match
	if len(allPods) == 0 {
		pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list pods: %w", err)
		}

		for _, pod := range pods.Items {
			// Check if pod name starts with appName (handles deployment naming)
			if len(pod.Name) >= len(appName) && pod.Name[:len(appName)] == appName {
				// Verify it's actually a match (app-xyz matches, app2-xyz doesn't)
				if len(pod.Name) == len(appName) || pod.Name[len(appName)] == '-' {
					info := podToPodInfo(&pod)
					allPods = append(allPods, info)
				}
			}
		}
	}

	if len(allPods) == 0 {
		return nil, fmt.Errorf("no pods found for %q in namespace %q", appName, namespace)
	}

	// Sort by name for consistent output
	sort.Slice(allPods, func(i, j int) bool {
		return allPods[i].Name < allPods[j].Name
	})

	return allPods, nil
}

func podToPodInfo(pod *corev1.Pod) PodInfo {
	info := PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Status:    string(pod.Status.Phase),
	}

	// Get the main container (first one, or the one that's not an init container)
	if len(pod.Spec.Containers) > 0 {
		info.ContainerName = pod.Spec.Containers[0].Name
	}

	// Check readiness and restarts
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == info.ContainerName {
			info.Ready = cs.Ready
			info.Restarts = cs.RestartCount
			break
		}
	}

	return info
}

// GetPodContainer returns the container name to use for a pod
// Prefers the main app container over sidecars
func GetPodContainer(ctx context.Context, client *kubernetes.Clientset, namespace, podName string) (string, error) {
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod: %w", err)
	}

	if len(pod.Spec.Containers) == 0 {
		return "", fmt.Errorf("pod has no containers")
	}

	// If only one container, use it
	if len(pod.Spec.Containers) == 1 {
		return pod.Spec.Containers[0].Name, nil
	}

	// Try to find the "main" container (not istio-proxy, envoy, etc.)
	sidecars := map[string]bool{
		"istio-proxy":    true,
		"envoy":          true,
		"linkerd-proxy":  true,
		"cloudsql-proxy": true,
	}

	for _, c := range pod.Spec.Containers {
		if !sidecars[c.Name] {
			return c.Name, nil
		}
	}

	// Fall back to first container
	return pod.Spec.Containers[0].Name, nil
}

// IsContainerRestarting checks if a pod's container is in a restart loop
func IsContainerRestarting(ctx context.Context, client *kubernetes.Clientset, namespace, podName, containerName string) (bool, error) {
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == containerName {
			// Consider restarting if:
			// - Has restarts and not ready
			// - Waiting with CrashLoopBackOff
			if cs.RestartCount > 0 && !cs.Ready {
				return true, nil
			}
			if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
				return true, nil
			}
		}
	}

	return false, nil
}
