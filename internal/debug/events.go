package debug

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// Event represents a Kubernetes event with enhanced metadata
type Event struct {
	Timestamp time.Time
	Type      string // "Normal", "Warning"
	Reason    string
	Object    string // "pod/myapp-abc12", "deployment/myapp"
	Message   string
	Count     int32
}

// EventFilter configures which events to show
type EventFilter struct {
	AppName string        // Filter by app label (kbox.dev/app)
	Types   []string      // Filter by event types: "Normal", "Warning"
	Since   time.Duration // Only events newer than this
}

// EventsOptions configures event streaming
type EventsOptions struct {
	Follow bool          // Stream new events (default: true)
	Filter EventFilter   // Event filtering
	Output io.Writer     // Output destination
}

// DefaultEventsOptions returns sensible defaults
func DefaultEventsOptions() EventsOptions {
	return EventsOptions{
		Follow: true,
	}
}

// StreamEvents streams Kubernetes events for an app
func StreamEvents(ctx context.Context, client *kubernetes.Clientset, namespace string, opts EventsOptions) error {
	// First, list existing events
	events, err := listAppEvents(ctx, client, namespace, opts.Filter)
	if err != nil {
		return fmt.Errorf("failed to list events: %w", err)
	}

	// Print existing events
	for _, event := range events {
		formatEvent(opts.Output, event)
	}

	// If not following, we're done
	if !opts.Follow {
		return nil
	}

	// Print separator
	if len(events) > 0 {
		fmt.Fprintln(opts.Output)
	}
	fmt.Fprintf(opts.Output, "\033[90mWatching for new events... (Ctrl+C to stop)\033[0m\n")
	fmt.Fprintln(opts.Output)

	// Watch for new events
	return watchAppEvents(ctx, client, namespace, opts)
}

// listAppEvents retrieves existing events for an app
func listAppEvents(ctx context.Context, client *kubernetes.Clientset, namespace string, filter EventFilter) ([]Event, error) {
	// List all events in namespace
	eventList, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var events []Event
	cutoff := time.Now().Add(-filter.Since)

	for _, e := range eventList.Items {
		// Filter by app if specified
		if filter.AppName != "" && !isAppEvent(&e, filter.AppName) {
			continue
		}

		// Filter by since
		eventTime := getEventTime(&e)
		if filter.Since > 0 && eventTime.Before(cutoff) {
			continue
		}

		// Filter by type
		if len(filter.Types) > 0 && !containsType(filter.Types, e.Type) {
			continue
		}

		events = append(events, Event{
			Timestamp: eventTime,
			Type:      e.Type,
			Reason:    e.Reason,
			Object:    formatObject(&e),
			Message:   e.Message,
			Count:     e.Count,
		})
	}

	// Sort by timestamp (newest last)
	sortEventsByTime(events)

	return events, nil
}

// watchAppEvents watches for new events
func watchAppEvents(ctx context.Context, client *kubernetes.Clientset, namespace string, opts EventsOptions) error {
	watcher, err := client.CoreV1().Events(namespace).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to watch events: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case watchEvent, ok := <-watcher.ResultChan():
			if !ok {
				// Watcher closed, try to restart
				return StreamEvents(ctx, client, namespace, opts)
			}

			if watchEvent.Type == watch.Added || watchEvent.Type == watch.Modified {
				if e, ok := watchEvent.Object.(*corev1.Event); ok {
					// Apply filters
					if opts.Filter.AppName != "" && !isAppEvent(e, opts.Filter.AppName) {
						continue
					}
					if len(opts.Filter.Types) > 0 && !containsType(opts.Filter.Types, e.Type) {
						continue
					}

					event := Event{
						Timestamp: getEventTime(e),
						Type:      e.Type,
						Reason:    e.Reason,
						Object:    formatObject(e),
						Message:   e.Message,
						Count:     e.Count,
					}
					formatEvent(opts.Output, event)
				}
			}
		}
	}
}

// isAppEvent checks if an event is related to an app
func isAppEvent(event *corev1.Event, appName string) bool {
	// Check if the involved object name contains the app name
	objName := event.InvolvedObject.Name

	// Match pod names like "myapp-6d4f8-abc12" or deployment "myapp"
	if strings.HasPrefix(objName, appName+"-") || objName == appName {
		return true
	}

	// Also check for ReplicaSet names like "myapp-6d4f8"
	if event.InvolvedObject.Kind == "ReplicaSet" && strings.HasPrefix(objName, appName+"-") {
		return true
	}

	return false
}

// getEventTime returns the most relevant timestamp for an event
func getEventTime(event *corev1.Event) time.Time {
	// Prefer LastTimestamp, fall back to FirstTimestamp, then EventTime
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.FirstTimestamp.IsZero() {
		return event.FirstTimestamp.Time
	}
	if event.EventTime.Time.IsZero() {
		return time.Now()
	}
	return event.EventTime.Time
}

// formatObject returns a readable string for the involved object
func formatObject(event *corev1.Event) string {
	kind := strings.ToLower(event.InvolvedObject.Kind)
	name := event.InvolvedObject.Name
	return fmt.Sprintf("%s/%s", kind, name)
}

// containsType checks if a type is in the filter list
func containsType(types []string, t string) bool {
	for _, typ := range types {
		if strings.EqualFold(typ, t) {
			return true
		}
	}
	return false
}

// sortEventsByTime sorts events by timestamp (oldest first)
func sortEventsByTime(events []Event) {
	for i := 0; i < len(events)-1; i++ {
		for j := i + 1; j < len(events); j++ {
			if events[i].Timestamp.After(events[j].Timestamp) {
				events[i], events[j] = events[j], events[i]
			}
		}
	}
}

// formatEvent writes a formatted event to output with colors
func formatEvent(w io.Writer, event Event) {
	// Time column (gray)
	timestamp := event.Timestamp.Format("15:04:05")

	// Type column (green for Normal, yellow for Warning)
	var typeColor string
	switch event.Type {
	case "Normal":
		typeColor = "\033[32m" // Green
	case "Warning":
		typeColor = "\033[33m" // Yellow
	default:
		typeColor = "\033[31m" // Red
	}

	// Build the formatted line
	fmt.Fprintf(w, "\033[90m%s\033[0m  %s%-7s\033[0m  \033[36m%-15s\033[0m  \033[90m%-25s\033[0m  %s",
		timestamp,
		typeColor,
		event.Type,
		truncate(event.Reason, 15),
		truncate(event.Object, 25),
		event.Message,
	)

	// Add count if > 1
	if event.Count > 1 {
		fmt.Fprintf(w, " \033[90m(x%d)\033[0m", event.Count)
	}

	fmt.Fprintln(w)
}

// truncate shortens a string to max length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// PrintEventsHeader prints the table header for events
func PrintEventsHeader(w io.Writer) {
	fmt.Fprintf(w, "\033[1m%-8s  %-7s  %-15s  %-25s  %s\033[0m\n",
		"TIME", "TYPE", "REASON", "OBJECT", "MESSAGE")
	fmt.Fprintln(w)
}
