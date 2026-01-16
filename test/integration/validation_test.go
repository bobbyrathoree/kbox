//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSSAForceConflictResolution tests Issue #1: SSA Force=true
// Verifies that kbox can update resources even when another controller
// has modified them (field ownership conflict resolution).
func TestSSAForceConflictResolution(t *testing.T) {
	appName := "ssa-test"
	defer cleanupApp(t, TestNamespace, appName)

	dockerfile := `FROM alpine:3.18
EXPOSE 8080
CMD ["sleep", "infinity"]`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: ssa-test
  namespace: ` + TestNamespace + `
spec:
  image: alpine:3.18
  port: 8080
  replicas: 1
  command: ["sleep", "infinity"]`

	dir := createTestApp(t, appName, dockerfile, kboxYaml, "")

	// First deploy
	stdout, stderr, err := runKbox(t, dir, "deploy", "--no-wait")
	if err != nil {
		t.Fatalf("First deploy failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Manually patch the deployment with kubectl (different field manager)
	// This simulates another controller modifying the resource
	patchCmd := []string{
		"kubectl", "patch", "deployment", appName,
		"-n", TestNamespace,
		"--type=merge",
		"-p", `{"metadata":{"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"test"}}}`,
	}
	_, _, _ = runKbox(t, "", patchCmd...)

	// Second deploy should succeed (Force=true resolves conflict)
	stdout, stderr, err = runKbox(t, dir, "deploy", "--no-wait")
	if err != nil {
		t.Fatalf("Second deploy failed with SSA conflict: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Verify deployment exists and is correct
	assertContains(t, stdout, "Deployment/ssa-test")
}

// TestFailFastOnSecretFailure tests Issue #2: Fail-fast on critical resources
// Verifies that when a Secret/ConfigMap fails, the deployment stops immediately
// without creating a partial state (e.g., Deployment without its ConfigMap).
func TestFailFastOnSecretFailure(t *testing.T) {
	appName := "failfast-test"
	defer cleanupApp(t, TestNamespace, appName)

	// kbox.yaml referencing a nonexistent .env file
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: failfast-test
  namespace: ` + TestNamespace + `
spec:
  image: alpine:3.18
  port: 8080
  secrets:
    fromEnvFile: nonexistent.env`

	dir := createTestApp(t, appName, "", kboxYaml, "")

	// Deploy should fail
	_, stderr, err := runKbox(t, dir, "deploy", "--no-wait")
	if err == nil {
		t.Fatal("Expected deploy to fail when .env file doesn't exist")
	}

	// Verify error mentions the missing file
	if !strings.Contains(stderr, "nonexistent.env") && !strings.Contains(stderr, "no such file") {
		t.Errorf("Expected error about missing .env file, got: %s", stderr)
	}

	// Verify Deployment was NOT created (fail-fast behavior)
	ctx := context.Background()
	_, err = k8sClient.AppsV1().Deployments(TestNamespace).Get(ctx, appName, metav1.GetOptions{})
	if err == nil {
		t.Error("Deployment should NOT exist after secret failure (fail-fast)")
	}
}

// TestResourcePruning tests Issue #4: Resource pruning
// Verifies that --prune flag removes orphaned resources.
func TestResourcePruning(t *testing.T) {
	appName := "prune-test"
	defer cleanupApp(t, TestNamespace, appName)

	dockerfile := `FROM alpine:3.18
EXPOSE 8080
CMD ["sleep", "infinity"]`

	// First config with env vars (creates ConfigMap)
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: prune-test
  namespace: ` + TestNamespace + `
spec:
  image: alpine:3.18
  port: 8080
  replicas: 1
  command: ["sleep", "infinity"]
  env:
    MY_VAR: test`

	dir := createTestApp(t, appName, dockerfile, kboxYaml, "")

	// First deploy - creates ConfigMap
	stdout, stderr, err := runKbox(t, dir, "deploy", "--no-wait")
	if err != nil {
		t.Fatalf("First deploy failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Verify ConfigMap exists
	ctx := context.Background()
	configMapName := appName + "-config"
	_, err = k8sClient.CoreV1().ConfigMaps(TestNamespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ConfigMap should exist after first deploy: %v", err)
	}

	// Update config - remove env vars
	kboxYamlNoEnv := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: prune-test
  namespace: ` + TestNamespace + `
spec:
  image: alpine:3.18
  port: 8080
  replicas: 1
  command: ["sleep", "infinity"]`

	dir = createTestApp(t, appName, dockerfile, kboxYamlNoEnv, "")

	// Deploy with --prune
	stdout, stderr, err = runKbox(t, dir, "deploy", "--no-wait", "--prune")
	if err != nil {
		t.Fatalf("Deploy with prune failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	// Wait a moment for pruning
	time.Sleep(2 * time.Second)

	// Verify ConfigMap was pruned
	_, err = k8sClient.CoreV1().ConfigMaps(TestNamespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err == nil {
		t.Error("ConfigMap should have been pruned")
	}
}

// TestNetworkPolicyExternalEgress tests Issue #5: External egress
// Note: This test requires a CNI with NetworkPolicy support (e.g., Calico).
// In kind, NetworkPolicies are not enforced by default.
func TestNetworkPolicyExternalEgress(t *testing.T) {
	t.Skip("Skipping: requires NetworkPolicy-capable CNI (not default in kind)")
}

// TestValidQuantitiesAccepted tests Issue #9: K8s quantity validation
// Verifies that valid resource quantities are accepted.
func TestValidQuantitiesAccepted(t *testing.T) {
	appName := "quantity-valid"
	defer cleanupApp(t, TestNamespace, appName)

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: quantity-valid
  namespace: ` + TestNamespace + `
spec:
  image: alpine:3.18
  port: 8080
  resources:
    memory: 128Mi
    cpu: 100m
    memoryLimit: 256Mi
    cpuLimit: 200m`

	dir := createTestApp(t, appName, "", kboxYaml, "")

	// Deploy should succeed
	stdout, stderr, err := runKbox(t, dir, "deploy", "--dry-run")
	if err != nil {
		t.Fatalf("Deploy with valid quantities failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
}

// TestInvalidQuantitiesRejected tests Issue #9: K8s quantity validation
// Verifies that invalid resource quantities are rejected at validation time.
func TestInvalidQuantitiesRejected(t *testing.T) {
	appName := "quantity-invalid"

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: quantity-invalid
  namespace: ` + TestNamespace + `
spec:
  image: alpine:3.18
  port: 8080
  resources:
    memory: 500z`

	dir := createTestApp(t, appName, "", kboxYaml, "")

	// Deploy should fail validation
	_, stderr, err := runKbox(t, dir, "deploy", "--dry-run")
	if err == nil {
		t.Fatal("Expected validation error for invalid quantity '500z'")
	}

	assertContains(t, stderr, "invalid Kubernetes quantity")
}

// TestRequestExceedsLimitRejected tests Issue #10: Request <= Limit validation
// Verifies that memory/cpu request > limit is rejected.
func TestRequestExceedsLimitRejected(t *testing.T) {
	appName := "limit-exceeded"

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: limit-exceeded
  namespace: ` + TestNamespace + `
spec:
  image: alpine:3.18
  port: 8080
  resources:
    memory: 512Mi
    memoryLimit: 128Mi`

	dir := createTestApp(t, appName, "", kboxYaml, "")

	// Deploy should fail validation
	_, stderr, err := runKbox(t, dir, "deploy", "--dry-run")
	if err == nil {
		t.Fatal("Expected validation error for memory request > limit")
	}

	assertContains(t, stderr, "exceeds limit")
}
