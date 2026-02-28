package debug

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DiagnoseFailure returns a formatted diagnosis report for a rollout failure.
// It pattern-matches on the failure reason, fetches warning events for the pod,
// and retrieves previous container logs to help the user understand what went wrong.
func DiagnoseFailure(ctx context.Context, client kubernetes.Interface, namespace, appName, podName, reason string) string {
	diagnosis, suggestion := diagnosisForReason(reason)

	// Fetch warning events for the pod
	events := fetchWarningEvents(ctx, client, namespace, podName)

	// Fetch previous container logs
	logs := fetchPrevContainerLogs(ctx, client, namespace, podName)

	return buildReport(diagnosis, suggestion, events, logs)
}

// diagnosisForReason returns a human-readable diagnosis and suggestion for a given failure reason.
func diagnosisForReason(reason string) (string, string) {
	switch reason {
	case "CrashLoopBackOff":
		return "Your app is crashing on startup.", "Check the error in the logs below."
	case "ImagePullBackOff", "ErrImagePull":
		return "Kubernetes can't pull your container image.", "Check the image name and registry credentials."
	case "OOMKilled":
		return "Your app ran out of memory.", "Increase resources.memoryLimit in kbox.yaml."
	case "CreateContainerConfigError":
		return "Container configuration is invalid.", "Check env vars and volume mounts in kbox.yaml."
	default:
		return "Deployment failed.", "Check the logs and events below."
	}
}

// fetchWarningEvents retrieves the last 5 warning events related to the given pod.
func fetchWarningEvents(ctx context.Context, client kubernetes.Interface, namespace, podName string) []string {
	eventList, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	var warnings []string
	for _, event := range eventList.Items {
		if event.Type != "Warning" {
			continue
		}
		if !strings.HasPrefix(event.InvolvedObject.Name, podName) {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("%s: %s", event.Reason, event.Message))
	}

	// Take last 5
	if len(warnings) > 5 {
		warnings = warnings[len(warnings)-5:]
	}

	return warnings
}

// fetchPrevContainerLogs retrieves the last 20 lines of previous container logs.
// Returns nil if previous logs are not available (e.g., container hasn't restarted yet).
func fetchPrevContainerLogs(ctx context.Context, client kubernetes.Interface, namespace, podName string) []string {
	tailLines := int64(20)
	req := client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Previous:  true,
		TailLines: &tailLines,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		// Previous logs might not exist — that's okay
		return nil
	}
	defer stream.Close()

	var lines []string
	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		line := stripTimestamp(scanner.Text())
		lines = append(lines, line)
	}

	return lines
}

// stripTimestamp removes a leading ISO 8601 timestamp from a log line if present.
// Example: "2024-01-14T10:00:00.000000000Z message" -> "message"
func stripTimestamp(line string) string {
	// Kubernetes log timestamps are 30 chars: 2024-01-14T10:00:00.000000000Z
	if len(line) > 30 && line[4] == '-' && line[7] == '-' && line[10] == 'T' {
		rest := line[30:]
		if len(rest) > 0 && rest[0] == ' ' {
			rest = rest[1:]
		}
		return rest
	}
	return line
}

// buildReport assembles the formatted diagnosis box content.
func buildReport(diagnosis, suggestion string, events, logs []string) string {
	var b strings.Builder

	b.WriteString("Why: ")
	b.WriteString(diagnosis)
	b.WriteString("\n")
	b.WriteString("Fix: ")
	b.WriteString(suggestion)

	if len(events) > 0 {
		b.WriteString("\n\nEvents:")
		for _, e := range events {
			b.WriteString("\n  ")
			b.WriteString(e)
		}
	}

	if len(logs) > 0 {
		b.WriteString("\n\nPrevious Logs (last 20 lines):")
		for _, l := range logs {
			b.WriteString("\n  ")
			b.WriteString(l)
		}
	}

	return b.String()
}
