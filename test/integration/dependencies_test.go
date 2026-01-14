//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAddPostgres(t *testing.T) {
	// Create a basic kbox.yaml
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: deps-test-app
spec:
  image: nginx:alpine
  port: 8080
`

	appDir := t.TempDir()
	configPath := filepath.Join(appDir, "kbox.yaml")
	os.WriteFile(configPath, []byte(kboxYaml), 0644)

	t.Run("add postgres modifies kbox.yaml", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "add", "postgres")
		if err != nil {
			t.Fatalf("Add postgres failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		// Read the updated config
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config: %v", err)
		}

		if !strings.Contains(string(content), "postgres") {
			t.Error("Expected postgres in updated kbox.yaml")
		}
		if !strings.Contains(string(content), "dependencies:") {
			t.Error("Expected dependencies section in kbox.yaml")
		}
	})

	t.Run("add postgres with version", func(t *testing.T) {
		// Reset config
		os.WriteFile(configPath, []byte(kboxYaml), 0644)

		stdout, stderr, err := runKbox(t, appDir, "add", "postgres:14")
		if err != nil {
			t.Fatalf("Add postgres:14 failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		content, _ := os.ReadFile(configPath)
		if !strings.Contains(string(content), "version: \"14\"") {
			t.Errorf("Expected version: 14 in config, got: %s", string(content))
		}
	})
}

func TestDeployWithPostgres(t *testing.T) {
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: pg-test-app
spec:
  image: nginx:alpine
  port: 8080
  dependencies:
    - type: postgres
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	// Clean up
	defer func() {
		client := getK8sClient(t)
		client.AppsV1().Deployments(TestNamespace).Delete(context.Background(), "pg-test-app", metav1.DeleteOptions{})
		client.AppsV1().StatefulSets(TestNamespace).Delete(context.Background(), "pg-test-app-postgres", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "pg-test-app", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "pg-test-app-postgres", metav1.DeleteOptions{})
		client.CoreV1().Secrets(TestNamespace).Delete(context.Background(), "pg-test-app-postgres", metav1.DeleteOptions{})
	}()

	t.Run("deploy creates StatefulSet for postgres", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "deploy", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Deploy failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		client := getK8sClient(t)

		// Check StatefulSet was created
		ss, err := client.AppsV1().StatefulSets(TestNamespace).Get(
			context.Background(), "pg-test-app-postgres", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("StatefulSet not found: %v", err)
		}

		if ss.Spec.Template.Spec.Containers[0].Image != "postgres:15-alpine" {
			t.Errorf("Expected postgres:15-alpine image, got %s", ss.Spec.Template.Spec.Containers[0].Image)
		}

		// Check Service was created
		_, err = client.CoreV1().Services(TestNamespace).Get(
			context.Background(), "pg-test-app-postgres", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Postgres service not found: %v", err)
		}

		// Check Secret was created
		secret, err := client.CoreV1().Secrets(TestNamespace).Get(
			context.Background(), "pg-test-app-postgres", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Postgres secret not found: %v", err)
		}
		if _, ok := secret.Data["POSTGRES_PASSWORD"]; !ok {
			t.Error("Expected POSTGRES_PASSWORD in secret")
		}
	})

	t.Run("app deployment has DATABASE_URL injected", func(t *testing.T) {
		client := getK8sClient(t)

		dep, err := client.AppsV1().Deployments(TestNamespace).Get(
			context.Background(), "pg-test-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("App deployment not found: %v", err)
		}

		// Check for injected env vars
		foundDBURL := false
		foundPGHost := false
		for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "DATABASE_URL" {
				foundDBURL = true
				if !strings.Contains(env.Value, "pg-test-app-postgres") {
					t.Errorf("DATABASE_URL doesn't contain service name: %s", env.Value)
				}
			}
			if env.Name == "PGHOST" {
				foundPGHost = true
				if env.Value != "pg-test-app-postgres" {
					t.Errorf("Expected PGHOST=pg-test-app-postgres, got %s", env.Value)
				}
			}
		}

		if !foundDBURL {
			t.Error("Expected DATABASE_URL env var to be injected")
		}
		if !foundPGHost {
			t.Error("Expected PGHOST env var to be injected")
		}
	})
}

func TestDeployWithRedis(t *testing.T) {
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: redis-test-app
spec:
  image: nginx:alpine
  port: 8080
  dependencies:
    - type: redis
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	// Clean up
	defer func() {
		client := getK8sClient(t)
		client.AppsV1().Deployments(TestNamespace).Delete(context.Background(), "redis-test-app", metav1.DeleteOptions{})
		client.AppsV1().StatefulSets(TestNamespace).Delete(context.Background(), "redis-test-app-redis", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "redis-test-app", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "redis-test-app-redis", metav1.DeleteOptions{})
	}()

	t.Run("deploy creates Redis StatefulSet", func(t *testing.T) {
		_, _, err := runKbox(t, appDir, "deploy", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Deploy failed: %v", err)
		}

		client := getK8sClient(t)

		// Check StatefulSet
		ss, err := client.AppsV1().StatefulSets(TestNamespace).Get(
			context.Background(), "redis-test-app-redis", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Redis StatefulSet not found: %v", err)
		}

		if !strings.Contains(ss.Spec.Template.Spec.Containers[0].Image, "redis") {
			t.Errorf("Expected redis image, got %s", ss.Spec.Template.Spec.Containers[0].Image)
		}
	})

	t.Run("app has REDIS_URL injected", func(t *testing.T) {
		client := getK8sClient(t)

		dep, err := client.AppsV1().Deployments(TestNamespace).Get(
			context.Background(), "redis-test-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("App deployment not found: %v", err)
		}

		foundRedisURL := false
		for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "REDIS_URL" {
				foundRedisURL = true
				if !strings.Contains(env.Value, "redis-test-app-redis") {
					t.Errorf("REDIS_URL doesn't contain service name: %s", env.Value)
				}
			}
		}

		if !foundRedisURL {
			t.Error("Expected REDIS_URL env var to be injected")
		}
	})
}

func TestMultipleDependencies(t *testing.T) {
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: multi-deps-app
spec:
  image: nginx:alpine
  port: 8080
  dependencies:
    - type: postgres
    - type: redis
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	// Clean up
	defer func() {
		client := getK8sClient(t)
		client.AppsV1().Deployments(TestNamespace).Delete(context.Background(), "multi-deps-app", metav1.DeleteOptions{})
		client.AppsV1().StatefulSets(TestNamespace).Delete(context.Background(), "multi-deps-app-postgres", metav1.DeleteOptions{})
		client.AppsV1().StatefulSets(TestNamespace).Delete(context.Background(), "multi-deps-app-redis", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "multi-deps-app", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "multi-deps-app-postgres", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "multi-deps-app-redis", metav1.DeleteOptions{})
		client.CoreV1().Secrets(TestNamespace).Delete(context.Background(), "multi-deps-app-postgres", metav1.DeleteOptions{})
	}()

	t.Run("deploy creates both StatefulSets", func(t *testing.T) {
		_, _, err := runKbox(t, appDir, "deploy", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Deploy failed: %v", err)
		}

		client := getK8sClient(t)

		// Check Postgres
		_, err = client.AppsV1().StatefulSets(TestNamespace).Get(
			context.Background(), "multi-deps-app-postgres", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Postgres StatefulSet not found: %v", err)
		}

		// Check Redis
		_, err = client.AppsV1().StatefulSets(TestNamespace).Get(
			context.Background(), "multi-deps-app-redis", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Redis StatefulSet not found: %v", err)
		}
	})

	t.Run("app has both DATABASE_URL and REDIS_URL", func(t *testing.T) {
		client := getK8sClient(t)

		dep, err := client.AppsV1().Deployments(TestNamespace).Get(
			context.Background(), "multi-deps-app", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("App deployment not found: %v", err)
		}

		foundDBURL := false
		foundRedisURL := false
		for _, env := range dep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "DATABASE_URL" {
				foundDBURL = true
			}
			if env.Name == "REDIS_URL" {
				foundRedisURL = true
			}
		}

		if !foundDBURL {
			t.Error("Expected DATABASE_URL env var")
		}
		if !foundRedisURL {
			t.Error("Expected REDIS_URL env var")
		}
	})
}

func TestRemoveDependency(t *testing.T) {
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: remove-test-app
spec:
  image: nginx:alpine
  port: 8080
  dependencies:
    - type: postgres
    - type: redis
`

	appDir := t.TempDir()
	configPath := filepath.Join(appDir, "kbox.yaml")
	os.WriteFile(configPath, []byte(kboxYaml), 0644)

	t.Run("remove postgres from config", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "remove", "postgres")
		if err != nil {
			t.Fatalf("Remove failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		content, _ := os.ReadFile(configPath)
		if strings.Contains(string(content), "postgres") {
			t.Error("Expected postgres to be removed from config")
		}
		if !strings.Contains(string(content), "redis") {
			t.Error("Expected redis to remain in config")
		}
	})
}

func TestRenderWithDependencies(t *testing.T) {
	kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: render-deps-app
spec:
  image: nginx:alpine
  port: 8080
  dependencies:
    - type: postgres
      storage: 5Gi
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	t.Run("render shows StatefulSet and Secret", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "render")
		if err != nil {
			t.Fatalf("Render failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "kind: StatefulSet") {
			t.Error("Expected StatefulSet in render output")
		}
		if !strings.Contains(stdout, "kind: Secret") {
			t.Error("Expected Secret in render output")
		}
		if !strings.Contains(stdout, "render-deps-app-postgres") {
			t.Error("Expected postgres service name in output")
		}
		if !strings.Contains(stdout, "5Gi") {
			t.Error("Expected custom storage size in output")
		}
	})
}

func TestAddErrors(t *testing.T) {
	t.Run("add unsupported dependency fails", func(t *testing.T) {
		kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: error-test-app
spec:
  image: nginx:alpine
  port: 8080
`
		appDir := t.TempDir()
		os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

		_, stderr, err := runKbox(t, appDir, "add", "unsupported-db")
		if err == nil {
			t.Error("Expected error for unsupported dependency")
		}
		if !strings.Contains(stderr, "unsupported") {
			t.Errorf("Expected 'unsupported' error, got: %s", stderr)
		}
	})

	t.Run("add duplicate dependency fails", func(t *testing.T) {
		kboxYaml := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: dup-test-app
spec:
  image: nginx:alpine
  port: 8080
  dependencies:
    - type: postgres
`
		appDir := t.TempDir()
		os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

		_, stderr, err := runKbox(t, appDir, "add", "postgres")
		if err == nil {
			t.Error("Expected error for duplicate dependency")
		}
		if !strings.Contains(stderr, "already") {
			t.Errorf("Expected 'already' error, got: %s", stderr)
		}
	})
}
