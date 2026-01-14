//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestFullConfigDeploy tests deploying with kbox.yaml
func TestFullConfigDeploy(t *testing.T) {
	dockerfile := `FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go build -o server main.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 3000
CMD ["./server"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: fullconfig-app
spec:
  image: fullconfig-app:latest
  build:
    dockerfile: Dockerfile
    context: .
  port: 3000
  replicas: 1
  healthCheck: /health
  env:
    APP_NAME: fullconfig-app
    ENVIRONMENT: test
    PORT: "3000"
  resources:
    memory: 64Mi
    cpu: 50m
`

	appDir := createTestApp(t, "fullconfig-app", dockerfile, kboxYaml, "")
	defer cleanupApp(t, "default", "fullconfig-app")

	t.Run("kbox up with full config", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "up", "--no-logs")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("kbox up failed: %v\nOutput: %s", err, combined)
		}

		// Should NOT say "No kbox.yaml found"
		assertNotContains(t, combined, "No kbox.yaml found")
		assertContains(t, combined, "is running!")
		assertContains(t, combined, "Release #1 saved")
	})

	t.Run("deployment has correct replicas", func(t *testing.T) {
		ctx := context.Background()

		dep, err := k8sClient.AppsV1().Deployments("default").Get(ctx, "fullconfig-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Deployment not found: %v", err)
		}

		if *dep.Spec.Replicas != 1 {
			t.Errorf("Expected 1 replica, got %d", *dep.Spec.Replicas)
		}
	})

	t.Run("deployment has health probes", func(t *testing.T) {
		ctx := context.Background()

		dep, err := k8sClient.AppsV1().Deployments("default").Get(ctx, "fullconfig-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Deployment not found: %v", err)
		}

		container := dep.Spec.Template.Spec.Containers[0]
		if container.LivenessProbe == nil {
			t.Error("Expected liveness probe to be set")
		}
		if container.ReadinessProbe == nil {
			t.Error("Expected readiness probe to be set")
		}
		if container.LivenessProbe != nil && container.LivenessProbe.HTTPGet.Path != "/health" {
			t.Errorf("Expected probe path /health, got %s", container.LivenessProbe.HTTPGet.Path)
		}
	})

	t.Run("configmap created with env vars", func(t *testing.T) {
		ctx := context.Background()

		cm, err := k8sClient.CoreV1().ConfigMaps("default").Get(ctx, "fullconfig-app-config", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("ConfigMap not found: %v", err)
		}

		if cm.Data["APP_NAME"] != "fullconfig-app" {
			t.Errorf("Expected APP_NAME=fullconfig-app, got %q", cm.Data["APP_NAME"])
		}
		if cm.Data["ENVIRONMENT"] != "test" {
			t.Errorf("Expected ENVIRONMENT=test, got %q", cm.Data["ENVIRONMENT"])
		}
	})

	t.Run("service has correct port", func(t *testing.T) {
		ctx := context.Background()

		svc, err := k8sClient.CoreV1().Services("default").Get(ctx, "fullconfig-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Service not found: %v", err)
		}

		if svc.Spec.Ports[0].Port != 3000 {
			t.Errorf("Expected port 3000, got %d", svc.Spec.Ports[0].Port)
		}
	})

	t.Run("pods are running", func(t *testing.T) {
		err := waitForPod(t, "default", "app=fullconfig-app", 90*time.Second)
		if err != nil {
			t.Fatalf("Pods not ready: %v", err)
		}

		// Verify we have 1 pod
		ctx := context.Background()
		pods, err := k8sClient.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
			LabelSelector: "app=fullconfig-app",
		})
		if err != nil {
			t.Fatalf("Failed to list pods: %v", err)
		}

		runningPods := 0
		for _, pod := range pods.Items {
			if pod.Status.Phase == "Running" {
				runningPods++
			}
		}
		if runningPods != 1 {
			t.Errorf("Expected 1 running pod, got %d", runningPods)
		}
	})
}

// TestSecretsFromEnvFile tests loading secrets from .env file
func TestSecretsFromEnvFile(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: secrets-app
spec:
  image: alpine:3.19
  port: 8080
  replicas: 1
  secrets:
    fromEnvFile: .env
`

	envFile := `DB_PASSWORD=super-secret-123
API_KEY=sk-test-key-456
`

	appDir := createTestApp(t, "secrets-app", dockerfile, kboxYaml, envFile)
	defer cleanupApp(t, "default", "secrets-app")

	t.Run("render creates secret from env file", func(t *testing.T) {
		stdout, _, err := runKbox(t, appDir, "render")
		if err != nil {
			t.Fatalf("kbox render failed: %v", err)
		}

		// Should contain Secret kind
		assertContains(t, stdout, "kind: Secret")
		assertContains(t, stdout, "name: secrets-app-secrets")

		// Should have base64 encoded values (not plaintext)
		assertNotContains(t, stdout, "super-secret-123")
		assertNotContains(t, stdout, "sk-test-key-456")
	})

	t.Run("deploy creates secret", func(t *testing.T) {
		_, _, err := runKbox(t, appDir, "deploy", "--no-wait")
		if err != nil {
			t.Fatalf("kbox deploy failed: %v", err)
		}

		ctx := context.Background()
		secret, err := k8sClient.CoreV1().Secrets("default").Get(ctx, "secrets-app-secrets", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Secret not found: %v", err)
		}

		// Verify secret data
		if string(secret.Data["DB_PASSWORD"]) != "super-secret-123" {
			t.Errorf("Expected DB_PASSWORD to be 'super-secret-123', got %q", string(secret.Data["DB_PASSWORD"]))
		}
		if string(secret.Data["API_KEY"]) != "sk-test-key-456" {
			t.Errorf("Expected API_KEY to be 'sk-test-key-456', got %q", string(secret.Data["API_KEY"]))
		}
	})
}

// TestEnvironmentOverlays tests environment-specific config overrides
func TestEnvironmentOverlays(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: overlay-app
spec:
  image: alpine:3.19
  port: 8080
  replicas: 1
  env:
    LOG_LEVEL: info

environments:
  dev:
    replicas: 1
    env:
      LOG_LEVEL: debug
  prod:
    replicas: 5
    env:
      LOG_LEVEL: warn
`

	appDir := createTestApp(t, "overlay-app", dockerfile, kboxYaml, "")

	t.Run("render with dev environment", func(t *testing.T) {
		stdout, _, err := runKbox(t, appDir, "render", "-e", "dev")
		if err != nil {
			t.Fatalf("kbox render failed: %v", err)
		}

		// Should have dev values
		assertContains(t, stdout, "replicas: 1")
		assertContains(t, stdout, "LOG_LEVEL: debug")
	})

	t.Run("render with prod environment", func(t *testing.T) {
		stdout, _, err := runKbox(t, appDir, "render", "-e", "prod")
		if err != nil {
			t.Fatalf("kbox render failed: %v", err)
		}

		// Should have prod values
		assertContains(t, stdout, "replicas: 5")
		assertContains(t, stdout, "LOG_LEVEL: warn")
	})

	t.Run("render without environment uses base", func(t *testing.T) {
		stdout, _, err := runKbox(t, appDir, "render")
		if err != nil {
			t.Fatalf("kbox render failed: %v", err)
		}

		// Should have base values
		assertContains(t, stdout, "replicas: 1")
		assertContains(t, stdout, "LOG_LEVEL: info")
	})
}
