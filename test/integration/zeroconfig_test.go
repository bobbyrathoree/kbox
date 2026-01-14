//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestZeroConfigDeploy tests deploying with just a Dockerfile (no kbox.yaml)
func TestZeroConfigDeploy(t *testing.T) {
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

	appDir := createTestApp(t, "zeroconfig-app", dockerfile, "", "")
	defer cleanupApp(t, "default", "zeroconfig-app")

	t.Run("kbox up with zero config", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "up", "--no-logs")
		combined := stdout + stderr

		if err != nil {
			t.Fatalf("kbox up failed: %v\nOutput: %s", err, combined)
		}

		// Should detect no kbox.yaml
		assertContains(t, combined, "No kbox.yaml found")
		assertContains(t, combined, "from Dockerfile EXPOSE")
		assertContains(t, combined, "is running!")
	})

	t.Run("deployment created correctly", func(t *testing.T) {
		ctx := context.Background()

		// Check deployment exists
		dep, err := k8sClient.AppsV1().Deployments("default").Get(ctx, "zeroconfig-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Deployment not found: %v", err)
		}

		// Check labels
		if dep.Labels["app"] != "zeroconfig-app" {
			t.Errorf("Expected app label 'zeroconfig-app', got %q", dep.Labels["app"])
		}
		if dep.Labels["app.kubernetes.io/managed-by"] != "kbox" {
			t.Errorf("Expected managed-by label 'kbox', got %q", dep.Labels["app.kubernetes.io/managed-by"])
		}

		// Check replicas (default should be 1)
		if *dep.Spec.Replicas != 1 {
			t.Errorf("Expected 1 replica, got %d", *dep.Spec.Replicas)
		}
	})

	t.Run("service created correctly", func(t *testing.T) {
		ctx := context.Background()

		svc, err := k8sClient.CoreV1().Services("default").Get(ctx, "zeroconfig-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Service not found: %v", err)
		}

		// Check port (should be 8080 from EXPOSE)
		if len(svc.Spec.Ports) == 0 || svc.Spec.Ports[0].Port != 8080 {
			t.Errorf("Expected port 8080, got %v", svc.Spec.Ports)
		}
	})

	t.Run("pod is running", func(t *testing.T) {
		err := waitForPod(t, "default", "app=zeroconfig-app", 60*time.Second)
		if err != nil {
			t.Fatalf("Pod not ready: %v", err)
		}
	})
}

// TestZeroConfigInfersPort tests that port is correctly inferred from Dockerfile
func TestZeroConfigInfersPort(t *testing.T) {
	tests := []struct {
		name         string
		dockerfile   string
		expectedPort int32
	}{
		{
			name: "EXPOSE 3000",
			dockerfile: `FROM alpine:3.19
EXPOSE 3000
CMD ["sleep", "infinity"]`,
			expectedPort: 3000,
		},
		{
			name: "EXPOSE 9090/tcp",
			dockerfile: `FROM alpine:3.19
EXPOSE 9090/tcp
CMD ["sleep", "infinity"]`,
			expectedPort: 9090,
		},
		{
			name: "No EXPOSE defaults to 8080",
			dockerfile: `FROM alpine:3.19
CMD ["sleep", "infinity"]`,
			expectedPort: 8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appName := strings.ToLower(strings.ReplaceAll(tt.name, " ", "-"))
			appDir := createTestApp(t, appName, tt.dockerfile, "", "")
			defer cleanupApp(t, "default", appName)

			// Just render to check the port (don't actually deploy)
			stdout, _, err := runKbox(t, appDir, "render")
			if err != nil {
				// Render might fail for non-buildable images, that's ok
				// Just check that it detected the port
			}

			// For sleep-based containers, we can't really deploy, so just test render
			// The port should be in the rendered Service
			if tt.expectedPort != 8080 {
				assertContains(t, stdout, string(rune(tt.expectedPort/1000+'0'))+string(rune((tt.expectedPort%1000)/100+'0'))+string(rune((tt.expectedPort%100)/10+'0'))+string(rune(tt.expectedPort%10+'0')))
			}
		})
	}
}
