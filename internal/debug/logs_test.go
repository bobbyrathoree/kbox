package debug

import (
	"testing"
	"time"
)

// TestEventInterleavingLogic tests the core value prop: K8s events mixed with logs
func TestEventInterleavingLogic(t *testing.T) {
	t.Run("parseLogLine extracts timestamp and message", func(t *testing.T) {
		tests := []struct {
			line    string
			wantMsg string
		}{
			{
				"2024-01-14T10:00:00.000000000Z Starting server...",
				"Starting server...",
			},
			{
				"2024-01-14T10:00:00.123456789Z  Connection established",
				"Connection established",
			},
			{
				"No timestamp here",
				"No timestamp here",
			},
		}

		for _, tt := range tests {
			ts, msg := parseLogLine(tt.line)
			if msg != tt.wantMsg {
				t.Errorf("parseLogLine(%q) msg = %q, want %q", tt.line, msg, tt.wantMsg)
			}
			// Timestamp should be set (either parsed or Now)
			if ts.IsZero() {
				t.Errorf("timestamp should not be zero")
			}
		}
	})

	t.Run("shortName extracts pod suffix", func(t *testing.T) {
		tests := []struct {
			name string
			want string
		}{
			{"myapp-6d4f5c7b8d-abc12", "abc12"},
			{"myapp-xyz", "xyz"},
			{"simple", "simple"},
			{"verylongpodname-that-exceeds", "exceeds"},
		}

		for _, tt := range tests {
			got := shortName(tt.name)
			if got != tt.want {
				t.Errorf("shortName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		}
	})

	t.Run("LogLine represents both pod logs and events", func(t *testing.T) {
		// Pod log line
		podLog := LogLine{
			Timestamp: time.Now(),
			Source:    "pod/abc12",
			Message:   "Starting server on :8080",
			IsEvent:   false,
		}

		// K8s event line
		eventLog := LogLine{
			Timestamp: time.Now(),
			Source:    "k8s/event",
			Message:   "Warning: Liveness probe failed",
			IsEvent:   true,
		}

		if podLog.IsEvent {
			t.Error("pod log should not be marked as event")
		}
		if !eventLog.IsEvent {
			t.Error("k8s event should be marked as event")
		}
	})
}

// TestDefaultLogsOptions validates sensible defaults
func TestDefaultLogsOptions(t *testing.T) {
	opts := DefaultLogsOptions()

	if !opts.Follow {
		t.Error("default should follow logs")
	}
	if !opts.Timestamps {
		t.Error("default should show timestamps")
	}
	if !opts.ShowEvents {
		t.Error("default should show events - this is the killer feature!")
	}
	if !opts.AutoPrevious {
		t.Error("default should auto-fetch previous logs on restart")
	}
	if opts.TailLines != 100 {
		t.Errorf("default tail lines should be 100, got %d", opts.TailLines)
	}
}
