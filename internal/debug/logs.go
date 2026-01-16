package debug

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// LogLine represents a single log line with metadata
type LogLine struct {
	Timestamp time.Time
	Source    string // "pod/name" or "k8s/event"
	Message   string
	IsEvent   bool
}

// LogsOptions configures log streaming
type LogsOptions struct {
	Follow       bool
	Timestamps   bool
	TailLines    int64
	Previous     bool
	AutoPrevious bool // Auto-fetch previous if container is restarting
	ShowEvents   bool // Interleave K8s events (the killer feature)
}

// DefaultLogsOptions returns sensible defaults
func DefaultLogsOptions() LogsOptions {
	return LogsOptions{
		Follow:       true,
		Timestamps:   true,
		TailLines:    100,
		AutoPrevious: true,
		ShowEvents:   true,
	}
}

// StreamLogs streams logs from multiple pods with event interleaving
func StreamLogs(ctx context.Context, client *kubernetes.Clientset, namespace string, pods []PodInfo, opts LogsOptions, output io.Writer) error {
	if len(pods) == 0 {
		return fmt.Errorf("no pods to stream logs from")
	}

	// Channel for all log lines (from pods and events)
	lines := make(chan LogLine, 100)
	var wg sync.WaitGroup

	// Start log streaming for each pod
	for _, pod := range pods {
		wg.Add(1)
		go func(p PodInfo) {
			defer wg.Done()
			streamPodLogs(ctx, client, p, opts, lines)
		}(pod)
	}

	// Start event watching if enabled
	if opts.ShowEvents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			watchEvents(ctx, client, namespace, pods, lines)
		}()
	}

	// Close lines channel when all goroutines are done
	go func() {
		wg.Wait()
		close(lines)
	}()

	// Output lines as they come
	for line := range lines {
		formatLine(output, line, opts, len(pods) > 1)
	}

	return nil
}

func streamPodLogs(ctx context.Context, client *kubernetes.Clientset, pod PodInfo, opts LogsOptions, lines chan<- LogLine) {
	// Check if we should fetch previous logs
	shouldGetPrevious := opts.Previous
	if opts.AutoPrevious && !opts.Previous {
		isRestarting, _ := IsContainerRestarting(ctx, client, pod.Namespace, pod.Name, pod.ContainerName)
		if isRestarting {
			shouldGetPrevious = true
			lines <- LogLine{
				Timestamp: time.Now(),
				Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
				Message:   fmt.Sprintf("[kbox] Container is restarting (restarts=%d), fetching previous logs first", pod.Restarts),
				IsEvent:   true,
			}
		}
	}

	// Fetch previous logs if needed
	if shouldGetPrevious {
		fetchPreviousLogs(ctx, client, pod, opts, lines)
	}

	// Stream current logs
	tailLines := opts.TailLines
	req := client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container:  pod.ContainerName,
		Follow:     opts.Follow,
		Timestamps: true, // Always get timestamps for ordering
		TailLines:  &tailLines,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		lines <- LogLine{
			Timestamp: time.Now(),
			Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
			Message:   fmt.Sprintf("[kbox] Failed to stream logs: %v", err),
			IsEvent:   true,
		}
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	// 1MB buffer for large stack traces (default is 64KB which can overflow)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		ts, msg := parseLogLine(line)
		lines <- LogLine{
			Timestamp: ts,
			Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
			Message:   msg,
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			lines <- LogLine{
				Timestamp: time.Now(),
				Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
				Message:   "[kbox] Warning: log line exceeded 1MB, truncated",
				IsEvent:   true,
			}
		} else {
			lines <- LogLine{
				Timestamp: time.Now(),
				Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
				Message:   fmt.Sprintf("[kbox] Log stream error: %v", err),
				IsEvent:   true,
			}
		}
	}
}

func fetchPreviousLogs(ctx context.Context, client *kubernetes.Clientset, pod PodInfo, opts LogsOptions, lines chan<- LogLine) {
	tailLines := opts.TailLines
	req := client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container:  pod.ContainerName,
		Previous:   true,
		Timestamps: true,
		TailLines:  &tailLines,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		// Previous logs might not exist, that's okay
		return
	}
	defer stream.Close()

	lines <- LogLine{
		Timestamp: time.Now(),
		Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
		Message:   "[kbox] === Previous container logs ===",
		IsEvent:   true,
	}

	scanner := bufio.NewScanner(stream)
	// 1MB buffer for large stack traces (default is 64KB which can overflow)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		ts, msg := parseLogLine(line)
		lines <- LogLine{
			Timestamp: ts,
			Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
			Message:   msg,
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			lines <- LogLine{
				Timestamp: time.Now(),
				Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
				Message:   "[kbox] Warning: log line exceeded 1MB, truncated",
				IsEvent:   true,
			}
		} else {
			lines <- LogLine{
				Timestamp: time.Now(),
				Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
				Message:   fmt.Sprintf("[kbox] Log stream error: %v", err),
				IsEvent:   true,
			}
		}
	}

	lines <- LogLine{
		Timestamp: time.Now(),
		Source:    fmt.Sprintf("pod/%s", shortName(pod.Name)),
		Message:   "[kbox] === Current container logs ===",
		IsEvent:   true,
	}
}

func watchEvents(ctx context.Context, client *kubernetes.Clientset, namespace string, pods []PodInfo, lines chan<- LogLine) {
	// Build a set of pod names to filter events
	podNames := make(map[string]bool)
	for _, p := range pods {
		podNames[p.Name] = true
	}

	// Watch events in the namespace
	watcher, err := client.CoreV1().Events(namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return
			}

			if event.Type == watch.Added || event.Type == watch.Modified {
				if e, ok := event.Object.(*corev1.Event); ok {
					// Filter to events for our pods
					if e.InvolvedObject.Kind == "Pod" && podNames[e.InvolvedObject.Name] {
						lines <- LogLine{
							Timestamp: e.LastTimestamp.Time,
							Source:    "k8s/event",
							Message:   fmt.Sprintf("%s: %s", e.Reason, e.Message),
							IsEvent:   true,
						}
					}
				}
			}
		}
	}
}

// parseLogLine extracts timestamp and message from a log line
func parseLogLine(line string) (time.Time, string) {
	// Kubernetes log format: 2024-01-14T10:00:00.000000000Z message
	if len(line) > 30 && line[4] == '-' && line[7] == '-' {
		tsStr := line[:30]
		if ts, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
			msg := line[31:]
			if len(msg) > 0 && msg[0] == ' ' {
				msg = msg[1:]
			}
			return ts, msg
		}
	}
	return time.Now(), line
}

// formatLine writes a formatted log line to output
func formatLine(w io.Writer, line LogLine, opts LogsOptions, multiPod bool) {
	var prefix string

	if multiPod || line.IsEvent {
		// Color coding: events in yellow, pods in cyan
		if line.IsEvent {
			prefix = fmt.Sprintf("\033[33m[%-12s]\033[0m ", line.Source) // Yellow
		} else {
			prefix = fmt.Sprintf("\033[36m[%-12s]\033[0m ", line.Source) // Cyan
		}
	}

	var timestamp string
	if opts.Timestamps && !line.Timestamp.IsZero() {
		timestamp = line.Timestamp.Format("15:04:05") + " "
	}

	fmt.Fprintf(w, "%s%s%s\n", prefix, timestamp, line.Message)
}

// shortName returns the last part of a pod name (after the last dash)
// e.g., "myapp-6d4f5c7b8d-abc12" -> "abc12"
func shortName(name string) string {
	lastDash := -1
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '-' {
			lastDash = i
			break
		}
	}
	if lastDash > 0 && lastDash < len(name)-1 {
		return name[lastDash+1:]
	}
	if len(name) > 12 {
		return name[:12]
	}
	return name
}
