package debug

import (
	"strings"
	"testing"
)

func TestDiagnosisForReason(t *testing.T) {
	tests := []struct {
		reason         string
		wantDiagnosis  string
		wantSuggestion string
	}{
		{
			reason:         "CrashLoopBackOff",
			wantDiagnosis:  "Your app is crashing on startup.",
			wantSuggestion: "Check the error in the logs below.",
		},
		{
			reason:         "ImagePullBackOff",
			wantDiagnosis:  "Kubernetes can't pull your container image.",
			wantSuggestion: "Check the image name and registry credentials.",
		},
		{
			reason:         "ErrImagePull",
			wantDiagnosis:  "Kubernetes can't pull your container image.",
			wantSuggestion: "Check the image name and registry credentials.",
		},
		{
			reason:         "OOMKilled",
			wantDiagnosis:  "Your app ran out of memory.",
			wantSuggestion: "Increase resources.memoryLimit in kbox.yaml.",
		},
		{
			reason:         "CreateContainerConfigError",
			wantDiagnosis:  "Container configuration is invalid.",
			wantSuggestion: "Check env vars and volume mounts in kbox.yaml.",
		},
		{
			reason:         "SomeUnknownReason",
			wantDiagnosis:  "Deployment failed.",
			wantSuggestion: "Check the logs and events below.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			diagnosis, suggestion := diagnosisForReason(tt.reason)
			if diagnosis != tt.wantDiagnosis {
				t.Errorf("diagnosisForReason(%q) diagnosis = %q, want %q", tt.reason, diagnosis, tt.wantDiagnosis)
			}
			if suggestion != tt.wantSuggestion {
				t.Errorf("diagnosisForReason(%q) suggestion = %q, want %q", tt.reason, suggestion, tt.wantSuggestion)
			}
		})
	}
}

func TestStripTimestamp(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "2024-01-14T10:00:00.000000000Z panic: runtime error",
			want:  "panic: runtime error",
		},
		{
			input: "2024-01-14T10:00:00.123456789Z  extra space",
			want:  " extra space",
		},
		{
			input: "no timestamp here",
			want:  "no timestamp here",
		},
		{
			input: "short",
			want:  "short",
		},
		{
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripTimestamp(tt.input)
			if got != tt.want {
				t.Errorf("stripTimestamp(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildReport(t *testing.T) {
	t.Run("with events and logs", func(t *testing.T) {
		report := buildReport(
			"Your app is crashing on startup.",
			"Check the error in the logs below.",
			[]string{"BackOff: Back-off restarting failed container"},
			[]string{"panic: runtime error: invalid memory address", "goroutine 1 [running]:"},
		)

		if !strings.Contains(report, "Why: Your app is crashing on startup.") {
			t.Error("report should contain diagnosis")
		}
		if !strings.Contains(report, "Fix: Check the error in the logs below.") {
			t.Error("report should contain suggestion")
		}
		if !strings.Contains(report, "Events:") {
			t.Error("report should contain events section")
		}
		if !strings.Contains(report, "BackOff: Back-off restarting failed container") {
			t.Error("report should contain event message")
		}
		if !strings.Contains(report, "Previous Logs (last 20 lines):") {
			t.Error("report should contain logs section")
		}
		if !strings.Contains(report, "panic: runtime error: invalid memory address") {
			t.Error("report should contain log line")
		}
	})

	t.Run("without events or logs", func(t *testing.T) {
		report := buildReport(
			"Kubernetes can't pull your container image.",
			"Check the image name and registry credentials.",
			nil,
			nil,
		)

		if !strings.Contains(report, "Why: Kubernetes can't pull your container image.") {
			t.Error("report should contain diagnosis")
		}
		if !strings.Contains(report, "Fix: Check the image name and registry credentials.") {
			t.Error("report should contain suggestion")
		}
		if strings.Contains(report, "Events:") {
			t.Error("report should not contain events section when no events")
		}
		if strings.Contains(report, "Previous Logs") {
			t.Error("report should not contain logs section when no logs")
		}
	})

	t.Run("with events only", func(t *testing.T) {
		report := buildReport(
			"Deployment failed.",
			"Check the logs and events below.",
			[]string{"FailedScheduling: 0/3 nodes are available"},
			nil,
		)

		if !strings.Contains(report, "Events:") {
			t.Error("report should contain events section")
		}
		if strings.Contains(report, "Previous Logs") {
			t.Error("report should not contain logs section when no logs")
		}
	})
}
