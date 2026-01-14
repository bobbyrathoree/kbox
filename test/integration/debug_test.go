//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"
)

// TestDebugCommands tests the debug commands (status, logs)
func TestDebugCommands(t *testing.T) {
	// First deploy a test app
	dockerfile := `FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go build -o server main.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: debug-app
spec:
  image: debug-app:latest
  build:
    dockerfile: Dockerfile
    context: .
  port: 8080
  replicas: 1
  env:
    APP_NAME: debug-app
`

	appDir := createTestApp(t, "debug-app", dockerfile, kboxYaml, "")
	defer cleanupApp(t, "default", "debug-app")

	// Deploy the app first
	_, _, err := runKbox(t, appDir, "up", "--no-logs")
	if err != nil {
		t.Fatalf("Failed to deploy test app: %v", err)
	}

	// Wait for pod to be ready
	err = waitForPod(t, "default", "app=debug-app", 60*time.Second)
	if err != nil {
		t.Fatalf("Pod not ready: %v", err)
	}

	t.Run("kbox status shows deployment info", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, "", "status", "debug-app")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("kbox status failed: %v\nOutput: %s", err, combined)
		}

		// Should show app info
		assertContains(t, combined, "debug-app")
		assertContains(t, combined, "Deployment:")
		assertContains(t, combined, "Replicas:")
		assertContains(t, combined, "Pods")
		assertContains(t, combined, "Running")
	})

	t.Run("kbox status shows events", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, "", "status", "debug-app")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("kbox status failed: %v", err)
		}

		// Should show recent events
		assertContains(t, combined, "Recent Events:")
	})

	t.Run("kbox logs streams pod logs", func(t *testing.T) {
		// Run logs with a short timeout (it normally follows forever)
		stdout, stderr, _ := runKboxWithInput(t, "", "", "logs", "debug-app", "--tail=10")
		combined := stdout + stderr

		// Should show streaming message
		assertContains(t, combined, "Streaming logs")
	})

	t.Run("kbox logs shows K8s events", func(t *testing.T) {
		stdout, stderr, _ := runKboxWithInput(t, "", "", "logs", "debug-app", "--tail=10")
		combined := stdout + stderr

		// Should mention K8s events feature
		assertContains(t, combined, "K8s events")
	})
}

// TestDoctorCommand tests the doctor command
func TestDoctorCommand(t *testing.T) {
	t.Run("doctor checks pass on valid setup", func(t *testing.T) {
		stdout, stderr, _ := runKbox(t, "", "doctor")
		combined := stdout + stderr

		// Should check for required tools
		assertContains(t, combined, "docker")
		assertContains(t, combined, "kubectl")

		// Should check cluster connection
		assertContains(t, combined, "cluster connection")

		// Should check permissions
		assertContains(t, combined, "permission")
	})
}

// TestDiffCommand tests the diff command
func TestDiffCommand(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: diff-app
spec:
  image: alpine:3.19
  port: 8080
  replicas: 1
`

	appDir := createTestApp(t, "diff-app", dockerfile, kboxYaml, "")
	defer cleanupApp(t, "default", "diff-app")

	t.Run("diff shows create for new app", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "diff")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("kbox diff failed: %v\nOutput: %s", err, combined)
		}

		// Should show resources to be created
		assertContains(t, combined, "create")
		assertContains(t, combined, "Service/diff-app")
		assertContains(t, combined, "Deployment/diff-app")
	})

	t.Run("diff shows no changes after deploy", func(t *testing.T) {
		// Deploy first
		_, _, err := runKbox(t, appDir, "deploy", "--no-wait")
		if err != nil {
			t.Fatalf("Deploy failed: %v", err)
		}

		// Now diff should show no changes
		stdout, stderr, err := runKbox(t, appDir, "diff")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("kbox diff failed: %v", err)
		}

		// Should indicate no changes or unchanged
		if !strings.Contains(combined, "No changes") && !strings.Contains(combined, "unchanged") {
			// At minimum, shouldn't say "create"
			if strings.Contains(combined, "(create)") {
				t.Errorf("Expected no create actions after deploy, got: %s", combined)
			}
		}
	})
}
