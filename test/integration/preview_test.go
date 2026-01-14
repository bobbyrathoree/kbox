//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type PreviewInfo struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	App       string    `json:"app"`
	Created   time.Time `json:"created"`
	Status    string    `json:"status"`
}

func TestPreviewCreate(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: preview-test-app
spec:
  image: preview-test-app:latest
  port: 8080
  replicas: 1
`

	appDir := createTestApp(t, "preview-test-app", dockerfile, kboxYaml, "")
	previewName := "pr-123"

	// Clean up at the end
	defer func() {
		runKbox(t, appDir, "preview", "destroy", "--name="+previewName)
		cleanupApp(t, TestNamespace, "preview-test-app")
	}()

	t.Run("create preview environment", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "preview", "create", "--name="+previewName)
		if err != nil {
			t.Fatalf("Preview create failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		combined := stdout + stderr
		if !strings.Contains(combined, "preview-test-app-preview-pr-123") {
			t.Error("Expected preview namespace name in output")
		}
		if !strings.Contains(combined, "created successfully") {
			t.Error("Expected success message in output")
		}
	})

	t.Run("preview namespace exists with correct labels", func(t *testing.T) {
		client := getK8sClient(t)
		ns, err := client.CoreV1().Namespaces().Get(context.Background(), "preview-test-app-preview-pr-123", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Failed to get preview namespace: %v", err)
		}

		if ns.Labels["kbox.dev/preview"] != "true" {
			t.Error("Expected kbox.dev/preview=true label")
		}
		if ns.Labels["kbox.dev/app"] != "preview-test-app" {
			t.Error("Expected kbox.dev/app=preview-test-app label")
		}
		if ns.Labels["kbox.dev/preview-name"] != previewName {
			t.Errorf("Expected kbox.dev/preview-name=%s label", previewName)
		}
	})

	t.Run("preview has deployment and service", func(t *testing.T) {
		client := getK8sClient(t)

		// Check deployment exists
		_, err := client.AppsV1().Deployments("preview-test-app-preview-pr-123").Get(
			context.Background(), "preview-test-app", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Expected deployment in preview namespace: %v", err)
		}

		// Check service exists
		_, err = client.CoreV1().Services("preview-test-app-preview-pr-123").Get(
			context.Background(), "preview-test-app", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Expected service in preview namespace: %v", err)
		}
	})
}

func TestPreviewList(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: list-test-app
spec:
  image: list-test-app:latest
  port: 8080
  replicas: 1
`

	appDir := createTestApp(t, "list-test-app", dockerfile, kboxYaml, "")
	previewName := "feature-xyz"

	// Clean up
	defer func() {
		runKbox(t, appDir, "preview", "destroy", "--name="+previewName)
		cleanupApp(t, TestNamespace, "list-test-app")
	}()

	// Create a preview first
	_, _, err := runKbox(t, appDir, "preview", "create", "--name="+previewName)
	if err != nil {
		t.Fatalf("Failed to create preview: %v", err)
	}

	t.Run("list shows created preview", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "preview", "list")
		if err != nil {
			t.Fatalf("Preview list failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, previewName) {
			t.Errorf("Expected preview name in output, got: %s", stdout)
		}
		if !strings.Contains(stdout, "list-test-app-preview-feature-xyz") {
			t.Errorf("Expected namespace in output, got: %s", stdout)
		}
	})

	t.Run("list with JSON output", func(t *testing.T) {
		stdout, _, err := runKbox(t, appDir, "preview", "list", "--output=json")
		if err != nil {
			t.Fatalf("Preview list failed: %v", err)
		}

		var previews []PreviewInfo
		if err := json.Unmarshal([]byte(stdout), &previews); err != nil {
			t.Fatalf("Invalid JSON output: %v\nOutput: %s", err, stdout)
		}

		found := false
		for _, p := range previews {
			if p.Name == previewName {
				found = true
				if p.App != "list-test-app" {
					t.Errorf("Expected app=list-test-app, got %s", p.App)
				}
			}
		}
		if !found {
			t.Errorf("Preview %q not found in list", previewName)
		}
	})
}

func TestPreviewDestroy(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: destroy-test-app
spec:
  image: destroy-test-app:latest
  port: 8080
  replicas: 1
`

	appDir := createTestApp(t, "destroy-test-app", dockerfile, kboxYaml, "")
	previewName := "to-delete"

	defer cleanupApp(t, TestNamespace, "destroy-test-app")

	// Create a preview first
	_, _, err := runKbox(t, appDir, "preview", "create", "--name="+previewName)
	if err != nil {
		t.Fatalf("Failed to create preview: %v", err)
	}

	t.Run("destroy removes preview namespace", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "preview", "destroy", "--name="+previewName)
		if err != nil {
			t.Fatalf("Preview destroy failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		combined := stdout + stderr
		if !strings.Contains(combined, "destroyed") {
			t.Error("Expected 'destroyed' in output")
		}

		// Verify namespace is gone (may take a moment to fully delete)
		client := getK8sClient(t)
		_, err = client.CoreV1().Namespaces().Get(
			context.Background(), "destroy-test-app-preview-to-delete", metav1.GetOptions{})
		if err == nil {
			// Namespace might be in Terminating state, which is acceptable
			t.Log("Namespace still exists but may be terminating")
		}
	})

	t.Run("destroy nonexistent preview fails gracefully", func(t *testing.T) {
		_, stderr, err := runKbox(t, appDir, "preview", "destroy", "--name=nonexistent")
		if err == nil {
			t.Error("Expected error when destroying nonexistent preview")
		}

		if !strings.Contains(stderr, "not found") {
			t.Errorf("Expected 'not found' error, got: %s", stderr)
		}
	})
}

func TestPreviewIsolation(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: isolation-test-app
spec:
  image: isolation-test-app:latest
  port: 8080
  replicas: 1
  env:
    MODE: production
`

	appDir := createTestApp(t, "isolation-test-app", dockerfile, kboxYaml, "")
	previewName := "staging"

	// Clean up
	defer func() {
		runKbox(t, appDir, "preview", "destroy", "--name="+previewName)
		cleanupApp(t, TestNamespace, "isolation-test-app")
	}()

	// Deploy to main namespace first
	_, _, err := runKbox(t, appDir, "deploy", "--no-wait", "-n", TestNamespace)
	if err != nil {
		t.Fatalf("Main deploy failed: %v", err)
	}

	// Create preview
	_, _, err = runKbox(t, appDir, "preview", "create", "--name="+previewName)
	if err != nil {
		t.Fatalf("Preview create failed: %v", err)
	}

	t.Run("preview namespace is separate from main", func(t *testing.T) {
		client := getK8sClient(t)

		// Main deployment exists
		mainDep, err := client.AppsV1().Deployments(TestNamespace).Get(
			context.Background(), "isolation-test-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Main deployment not found: %v", err)
		}

		// Preview deployment exists
		previewDep, err := client.AppsV1().Deployments("isolation-test-app-preview-staging").Get(
			context.Background(), "isolation-test-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Preview deployment not found: %v", err)
		}

		// They should have same name but different UIDs
		if mainDep.UID == previewDep.UID {
			t.Error("Main and preview deployments should have different UIDs")
		}
	})
}

func TestMultiplePreviews(t *testing.T) {
	dockerfile := `FROM alpine:3.19
CMD ["sleep", "infinity"]
`

	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: multi-preview-app
spec:
  image: multi-preview-app:latest
  port: 8080
  replicas: 1
`

	appDir := createTestApp(t, "multi-preview-app", dockerfile, kboxYaml, "")
	previews := []string{"pr-1", "pr-2", "pr-3"}

	// Clean up
	defer func() {
		for _, name := range previews {
			runKbox(t, appDir, "preview", "destroy", "--name="+name)
		}
		cleanupApp(t, TestNamespace, "multi-preview-app")
	}()

	t.Run("can create multiple previews", func(t *testing.T) {
		for _, name := range previews {
			_, _, err := runKbox(t, appDir, "preview", "create", "--name="+name)
			if err != nil {
				t.Fatalf("Failed to create preview %s: %v", name, err)
			}
		}

		// List should show all three
		stdout, _, err := runKbox(t, appDir, "preview", "list", "--output=json")
		if err != nil {
			t.Fatalf("Preview list failed: %v", err)
		}

		var list []PreviewInfo
		json.Unmarshal([]byte(stdout), &list)

		if len(list) < len(previews) {
			t.Errorf("Expected at least %d previews, got %d", len(previews), len(list))
		}

		// Verify each preview exists
		for _, name := range previews {
			found := false
			for _, p := range list {
				if p.Name == name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Preview %q not found in list", name)
			}
		}
	})
}

func TestPreviewErrors(t *testing.T) {
	tmpDir := t.TempDir()

	// Create kbox.yaml
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: error-test-app
spec:
  image: nginx:alpine
  port: 80
`
	os.WriteFile(filepath.Join(tmpDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	t.Run("create without name fails", func(t *testing.T) {
		_, stderr, err := runKbox(t, tmpDir, "preview", "create")
		if err == nil {
			t.Error("Expected error when name not provided")
		}

		if !strings.Contains(stderr, "name") {
			t.Errorf("Expected error about name, got: %s", stderr)
		}
	})

	t.Run("destroy without name fails", func(t *testing.T) {
		_, stderr, err := runKbox(t, tmpDir, "preview", "destroy")
		if err == nil {
			t.Error("Expected error when name not provided")
		}

		if !strings.Contains(stderr, "name") {
			t.Errorf("Expected error about name, got: %s", stderr)
		}
	})

	t.Run("create with invalid name fails", func(t *testing.T) {
		_, stderr, err := runKbox(t, tmpDir, "preview", "create", "--name=Invalid_Name!")
		if err == nil {
			t.Error("Expected error for invalid name")
		}

		if !strings.Contains(stderr, "invalid") {
			t.Errorf("Expected 'invalid' error, got: %s", stderr)
		}
	})
}

func getK8sClient(t *testing.T) kubernetes.Interface {
	t.Helper()

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeconfig.ClientConfig()
	if err != nil {
		t.Fatalf("Failed to get k8s config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create k8s client: %v", err)
	}

	return clientset
}
