//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// KindClusterName is the name of the kind cluster for integration tests
	KindClusterName = "kbox-integration-test"
	// TestNamespace is the namespace for integration tests
	TestNamespace = "kbox-test"
	// DefaultTimeout for operations
	DefaultTimeout = 2 * time.Minute
)

var (
	// kboxBinary is the path to the kbox binary
	kboxBinary string
	// k8sClient is the Kubernetes client
	k8sClient *kubernetes.Clientset
	// projectRoot is the root of the kbox project
	projectRoot string
)

// TestMain sets up the integration test environment
func TestMain(m *testing.M) {
	// Find project root
	var err error
	projectRoot, err = findProjectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to find project root: %v\n", err)
		os.Exit(1)
	}

	// Build kbox binary
	kboxBinary = filepath.Join(projectRoot, "kbox-test")
	fmt.Println("Building kbox binary...")
	if err := buildKbox(kboxBinary); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to build kbox: %v\n", err)
		os.Exit(1)
	}

	// Ensure kind cluster exists
	fmt.Println("Ensuring kind cluster exists...")
	if err := ensureKindCluster(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to setup kind cluster: %v\n", err)
		os.Exit(1)
	}

	// Create Kubernetes client
	fmt.Println("Creating Kubernetes client...")
	k8sClient, err = createK8sClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create k8s client: %v\n", err)
		os.Exit(1)
	}

	// Create test namespace
	fmt.Println("Setting up test namespace...")
	if err := setupNamespace(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to setup namespace: %v\n", err)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	fmt.Println("Cleaning up...")
	cleanup()

	os.Exit(code)
}

func findProjectRoot() (string, error) {
	// Start from current directory and walk up looking for go.mod
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find project root (go.mod)")
		}
		dir = parent
	}
}

func buildKbox(outputPath string) error {
	cmd := exec.Command("go", "build", "-o", outputPath, "./cmd/kbox")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func ensureKindCluster() error {
	// Check if cluster exists
	cmd := exec.Command("kind", "get", "clusters")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get kind clusters: %w", err)
	}

	clusters := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, cluster := range clusters {
		if cluster == KindClusterName {
			fmt.Printf("Kind cluster %s already exists\n", KindClusterName)
			// Set kubectl context
			exec.Command("kubectl", "config", "use-context", "kind-"+KindClusterName).Run()
			return nil
		}
	}

	// Create cluster
	fmt.Printf("Creating kind cluster %s...\n", KindClusterName)
	cmd = exec.Command("kind", "create", "cluster", "--name", KindClusterName, "--wait", "60s")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func createK8sClient() (*kubernetes.Clientset, error) {
	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func setupNamespace() error {
	ctx := context.Background()

	// Delete namespace if exists (clean slate)
	k8sClient.CoreV1().Namespaces().Delete(ctx, TestNamespace, metav1.DeleteOptions{})

	// Wait for deletion
	time.Sleep(2 * time.Second)

	// Create namespace using kubectl (simpler)
	cmd := exec.Command("kubectl", "create", "namespace", TestNamespace)
	cmd.Run() // Ignore error if already exists

	return nil
}

func cleanup() {
	// Clean up test namespace
	ctx := context.Background()
	k8sClient.CoreV1().Namespaces().Delete(ctx, TestNamespace, metav1.DeleteOptions{})

	// Remove test binary
	os.Remove(kboxBinary)

	// Note: We don't delete the kind cluster to speed up repeated test runs
	// To delete: kind delete cluster --name kbox-integration-test
}

// runKbox runs kbox with the given arguments and returns stdout, stderr, and error
func runKbox(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, kboxBinary, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// runKboxWithInput runs kbox with stdin input
func runKboxWithInput(t *testing.T, dir string, input string, args ...string) (string, string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, kboxBinary, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Stdin = strings.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// waitForPod waits for a pod with the given label to be ready
func waitForPod(t *testing.T, namespace, labelSelector string, timeout time.Duration) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for pod with selector %s", labelSelector)
		default:
			pods, err := k8sClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			if err != nil {
				time.Sleep(time.Second)
				continue
			}

			allReady := len(pods.Items) > 0
			for _, pod := range pods.Items {
				if pod.Status.Phase != "Running" {
					allReady = false
					break
				}
				for _, cond := range pod.Status.Conditions {
					if cond.Type == "Ready" && cond.Status != "True" {
						allReady = false
						break
					}
				}
			}

			if allReady {
				return nil
			}
			time.Sleep(time.Second)
		}
	}
}

// cleanupApp removes all resources for an app
func cleanupApp(t *testing.T, namespace, appName string) {
	t.Helper()
	ctx := context.Background()

	// Delete deployment
	k8sClient.AppsV1().Deployments(namespace).Delete(ctx, appName, metav1.DeleteOptions{})

	// Delete service
	k8sClient.CoreV1().Services(namespace).Delete(ctx, appName, metav1.DeleteOptions{})

	// Delete configmaps
	k8sClient.CoreV1().ConfigMaps(namespace).Delete(ctx, appName+"-config", metav1.DeleteOptions{})
	k8sClient.CoreV1().ConfigMaps(namespace).Delete(ctx, appName+"-releases", metav1.DeleteOptions{})

	// Delete secrets
	k8sClient.CoreV1().Secrets(namespace).Delete(ctx, appName+"-secrets", metav1.DeleteOptions{})

	// Wait for cleanup
	time.Sleep(2 * time.Second)
}

// createTestApp creates a temporary directory with a test app
// For zero-config tests (no kbox.yaml), it creates a subdirectory with the given name
// so that kbox infers the correct app name from the directory name.
func createTestApp(t *testing.T, name string, dockerfile string, kboxYaml string, envFile string) string {
	t.Helper()

	baseDir := t.TempDir()

	// For zero-config mode (no kbox.yaml), create a named subdirectory
	// so kbox infers the app name correctly from the directory name
	var dir string
	if kboxYaml == "" {
		dir = filepath.Join(baseDir, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create app directory: %v", err)
		}
	} else {
		dir = baseDir
	}

	if dockerfile != "" {
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
			t.Fatalf("Failed to write Dockerfile: %v", err)
		}
	}

	if kboxYaml != "" {
		if err := os.WriteFile(filepath.Join(dir, "kbox.yaml"), []byte(kboxYaml), 0644); err != nil {
			t.Fatalf("Failed to write kbox.yaml: %v", err)
		}
	}

	if envFile != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envFile), 0644); err != nil {
			t.Fatalf("Failed to write .env: %v", err)
		}
	}

	// Create a simple main.go for the test app
	mainGo := `package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from %s", os.Getenv("APP_NAME"))
	})
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	http.ListenAndServe(":"+port, nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("Failed to write main.go: %v", err)
	}

	return dir
}

// assertContains checks if output contains expected string
func assertContains(t *testing.T, output, expected string) {
	t.Helper()
	if !strings.Contains(output, expected) {
		t.Errorf("Expected output to contain %q, got:\n%s", expected, output)
	}
}

// assertNotContains checks if output does not contain string
func assertNotContains(t *testing.T, output, notExpected string) {
	t.Helper()
	if strings.Contains(output, notExpected) {
		t.Errorf("Expected output NOT to contain %q, got:\n%s", notExpected, output)
	}
}
