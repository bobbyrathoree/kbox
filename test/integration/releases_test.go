//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestReleaseHistory tests the history command
func TestReleaseHistory(t *testing.T) {
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
  name: history-app
spec:
  image: history-app:latest
  build:
    dockerfile: Dockerfile
    context: .
  port: 8080
  replicas: 1
  env:
    VERSION: v1
`

	appDir := createTestApp(t, "history-app", dockerfile, kboxYaml, "")
	defer cleanupApp(t, "default", "history-app")

	t.Run("history is empty before deploy", func(t *testing.T) {
		stdout, stderr, _ := runKbox(t, "", "history", "history-app")
		combined := stdout + stderr

		// Should indicate no releases
		if !strings.Contains(combined, "No releases") && !strings.Contains(combined, "no releases") {
			// Or it might just be empty
			if strings.Contains(combined, "#1") {
				t.Errorf("Expected no releases before deploy, got: %s", combined)
			}
		}
	})

	t.Run("history shows release after up", func(t *testing.T) {
		// Deploy using up (which saves releases)
		_, _, err := runKbox(t, appDir, "up", "--no-logs")
		if err != nil {
			t.Fatalf("Deploy failed: %v", err)
		}

		// Wait for pod
		err = waitForPod(t, "default", "app=history-app", 60*time.Second)
		if err != nil {
			t.Fatalf("Pod not ready: %v", err)
		}

		// Check history
		stdout, stderr, err := runKbox(t, "", "history", "history-app")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("kbox history failed: %v\nOutput: %s", err, combined)
		}

		// Should show release #1 (format is "#1" in table, not "Release #1")
		assertContains(t, combined, "#1")
		assertContains(t, combined, "history-app")
	})

	t.Run("history shows multiple releases after multiple ups", func(t *testing.T) {
		// Update config and run up again (creates release #2)
		kboxYamlV2 := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: history-app
spec:
  image: history-app:latest
  build:
    dockerfile: Dockerfile
    context: .
  port: 8080
  replicas: 1
  env:
    VERSION: v2
`
		if err := writeFile(appDir, "kbox.yaml", kboxYamlV2); err != nil {
			t.Fatalf("Failed to update kbox.yaml: %v", err)
		}

		_, _, err := runKbox(t, appDir, "up", "--no-logs")
		if err != nil {
			t.Fatalf("Second up failed: %v", err)
		}

		// Check history
		stdout, stderr, err := runKbox(t, "", "history", "history-app")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("kbox history failed: %v", err)
		}

		// Should show both releases
		assertContains(t, combined, "#1")
		assertContains(t, combined, "#2")
	})
}

// TestRollback tests the rollback command
func TestRollback(t *testing.T) {
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

	kboxYamlV1 := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: rollback-app
spec:
  image: rollback-app:latest
  build:
    dockerfile: Dockerfile
    context: .
  port: 8080
  replicas: 1
  env:
    VERSION: v1
`

	appDir := createTestApp(t, "rollback-app", dockerfile, kboxYamlV1, "")
	defer cleanupApp(t, "default", "rollback-app")

	t.Run("deploy v1 with up", func(t *testing.T) {
		_, _, err := runKbox(t, appDir, "up", "--no-logs")
		if err != nil {
			t.Fatalf("Deploy v1 failed: %v", err)
		}

		err = waitForPod(t, "default", "app=rollback-app", 60*time.Second)
		if err != nil {
			t.Fatalf("Pod not ready: %v", err)
		}

		// Verify v1 is deployed (check configmap)
		ctx := context.Background()
		cm, err := k8sClient.CoreV1().ConfigMaps("default").Get(ctx, "rollback-app-config", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("ConfigMap not found: %v", err)
		}

		if cm.Data["VERSION"] != "v1" {
			t.Errorf("Expected VERSION=v1, got %q", cm.Data["VERSION"])
		}
	})

	t.Run("deploy v2 with up", func(t *testing.T) {
		// Update kbox.yaml to v2
		kboxYamlV2 := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: rollback-app
spec:
  image: rollback-app:latest
  build:
    dockerfile: Dockerfile
    context: .
  port: 8080
  replicas: 1
  env:
    VERSION: v2
`
		// Write updated config
		if err := writeFile(appDir, "kbox.yaml", kboxYamlV2); err != nil {
			t.Fatalf("Failed to update kbox.yaml: %v", err)
		}

		// Use up instead of deploy to save release
		_, _, err := runKbox(t, appDir, "up", "--no-logs")
		if err != nil {
			t.Fatalf("Deploy v2 failed: %v", err)
		}

		// Wait for pod
		time.Sleep(3 * time.Second)

		// Verify v2 is deployed
		ctx := context.Background()
		cm, err := k8sClient.CoreV1().ConfigMaps("default").Get(ctx, "rollback-app-config", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("ConfigMap not found: %v", err)
		}

		if cm.Data["VERSION"] != "v2" {
			t.Errorf("Expected VERSION=v2, got %q", cm.Data["VERSION"])
		}
	})

	t.Run("rollback to v1", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, "", "rollback", "rollback-app")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("Rollback failed: %v\nOutput: %s", err, combined)
		}

		// Should indicate rollback
		if !strings.Contains(combined, "Rolling back") && !strings.Contains(combined, "rollback") {
			t.Logf("Rollback output: %s", combined)
		}

		// Wait for rollout
		time.Sleep(5 * time.Second)

		// Verify v1 is restored
		ctx := context.Background()
		cm, err := k8sClient.CoreV1().ConfigMaps("default").Get(ctx, "rollback-app-config", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("ConfigMap not found: %v", err)
		}

		if cm.Data["VERSION"] != "v1" {
			t.Errorf("Expected VERSION=v1 after rollback, got %q", cm.Data["VERSION"])
		}
	})

	t.Run("rollback to specific release", func(t *testing.T) {
		// First deploy v3 with up to create another release
		kboxYamlV3 := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: rollback-app
spec:
  image: rollback-app:latest
  build:
    dockerfile: Dockerfile
    context: .
  port: 8080
  replicas: 1
  env:
    VERSION: v3
`
		if err := writeFile(appDir, "kbox.yaml", kboxYamlV3); err != nil {
			t.Fatalf("Failed to update kbox.yaml: %v", err)
		}

		_, _, err := runKbox(t, appDir, "up", "--no-logs")
		if err != nil {
			t.Fatalf("Deploy v3 failed: %v", err)
		}

		// Wait for pod
		time.Sleep(3 * time.Second)

		// Rollback to release #2 (v2)
		stdout, stderr, err := runKbox(t, "", "rollback", "rollback-app", "--to", "2")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("Rollback to #2 failed: %v\nOutput: %s", err, combined)
		}

		// Wait for rollout
		time.Sleep(5 * time.Second)

		// Verify v2 is restored
		ctx := context.Background()
		cm, err := k8sClient.CoreV1().ConfigMaps("default").Get(ctx, "rollback-app-config", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("ConfigMap not found: %v", err)
		}

		if cm.Data["VERSION"] != "v2" {
			t.Errorf("Expected VERSION=v2 after rollback to #2, got %q", cm.Data["VERSION"])
		}
	})
}

// writeFile writes content to a file in the given directory
func writeFile(dir, filename, content string) error {
	return os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644)
}
