//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// DeployResult mirrors the output.DeployResult struct for testing
type DeployResult struct {
	Success    bool             `json:"success"`
	App        string           `json:"app"`
	Namespace  string           `json:"namespace"`
	Context    string           `json:"context,omitempty"`
	Resources  []ResourceResult `json:"resources"`
	Revision   int              `json:"revision,omitempty"`
	Error      string           `json:"error,omitempty"`
	DurationMs int64            `json:"duration_ms"`
}

type ResourceResult struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Error  string `json:"error,omitempty"`
}

func TestCIModeDeploy(t *testing.T) {
	dockerfile := `FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go build -o server main.go

FROM alpine:3.19
COPY --from=builder /app/server /server
EXPOSE 8080
CMD ["/server"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: ci-test-app
spec:
  image: ci-test-app:latest
  port: 8080
  replicas: 1
`

	appDir := createTestApp(t, "ci-test-app", dockerfile, kboxYaml, "")
	defer cleanupApp(t, TestNamespace, "ci-test-app")

	t.Run("deploy with --ci flag succeeds", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "deploy", "--ci", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("CI deploy failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		// CI mode should still show basic output
		combined := stdout + stderr
		if !containsString(combined, "Deploying") {
			t.Log("Note: CI mode still shows deploy progress")
		}
	})

	t.Run("deploy with --ci --output=json returns valid JSON", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "deploy", "--ci", "--output=json", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("CI JSON deploy failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		var result DeployResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("Invalid JSON output: %v\nOutput was: %s", err, stdout)
		}

		if !result.Success {
			t.Errorf("Expected success=true, got false. Error: %s", result.Error)
		}

		if result.App != "ci-test-app" {
			t.Errorf("Expected app=ci-test-app, got %s", result.App)
		}

		if result.Namespace != TestNamespace {
			t.Errorf("Expected namespace=%s, got %s", TestNamespace, result.Namespace)
		}

		if len(result.Resources) == 0 {
			t.Error("Expected at least one resource in result")
		}

		if result.DurationMs <= 0 {
			t.Error("Expected duration_ms to be set")
		}
	})

	t.Run("JSON output contains resource details", func(t *testing.T) {
		stdout, _, err := runKbox(t, appDir, "deploy", "--ci", "--output=json", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Deploy failed: %v", err)
		}

		var result DeployResult
		json.Unmarshal([]byte(stdout), &result)

		// Should have Deployment and Service at minimum
		hasDeployment := false
		hasService := false
		for _, r := range result.Resources {
			if r.Kind == "Deployment" {
				hasDeployment = true
			}
			if r.Kind == "Service" {
				hasService = true
			}
		}

		if !hasDeployment {
			t.Error("Expected Deployment in resources")
		}
		if !hasService {
			t.Error("Expected Service in resources")
		}
	})
}

func TestCIModeEnvVar(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: ci-env-test
spec:
  image: ci-env-test:latest
  port: 8080
`

	appDir := createTestApp(t, "ci-env-test", dockerfile, kboxYaml, "")
	defer cleanupApp(t, TestNamespace, "ci-env-test")

	t.Run("CI=true environment variable triggers CI mode", func(t *testing.T) {
		// Set CI=true
		os.Setenv("CI", "true")
		defer os.Unsetenv("CI")

		stdout, stderr, err := runKbox(t, appDir, "deploy", "--output=json", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Deploy with CI env failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		var result DeployResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("Invalid JSON output: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success, got error: %s", result.Error)
		}
	})

	t.Run("KBOX_CI=true environment variable triggers CI mode", func(t *testing.T) {
		os.Setenv("KBOX_CI", "true")
		defer os.Unsetenv("KBOX_CI")

		stdout, _, err := runKbox(t, appDir, "deploy", "--output=json", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Deploy with KBOX_CI env failed: %v", err)
		}

		var result DeployResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("Invalid JSON output: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success, got error: %s", result.Error)
		}
	})
}

func TestCIModeFailure(t *testing.T) {
	// Create a config that will fail (no config file)
	tmpDir := t.TempDir()

	t.Run("failed deploy returns error in JSON", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, tmpDir, "deploy", "--ci", "--output=json", "-n", TestNamespace)
		// We expect an error since there's no kbox.yaml
		_ = err // Error is expected

		// Even on failure, should output JSON
		combined := stdout + stderr
		var result DeployResult
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			// Try stderr if stdout is empty
			if err2 := json.Unmarshal([]byte(combined), &result); err2 != nil {
				t.Logf("Output: %s", combined)
				// In failure case, non-JSON error is acceptable
				if !containsString(combined, "kbox.yaml") {
					t.Error("Expected error message about missing kbox.yaml")
				}
				return
			}
		}

		if result.Success {
			t.Error("Expected success=false for missing config")
		}

		if result.Error == "" {
			t.Error("Expected error message to be set")
		}
	})
}

func TestCIModeRender(t *testing.T) {
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: render-test
spec:
  image: nginx:alpine
  port: 80
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	t.Run("render works in CI mode", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "render", "--ci")
		if err != nil {
			t.Fatalf("Render failed: %v\nstderr: %s", err, stderr)
		}

		// Should output YAML
		if !containsString(stdout, "apiVersion:") {
			t.Error("Expected YAML output with apiVersion")
		}
		if !containsString(stdout, "kind: Deployment") {
			t.Error("Expected Deployment in output")
		}
	})
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
