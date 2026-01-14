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

func TestMultiServiceDeploy(t *testing.T) {
	// Create multi-service kbox.yaml with api and web services
	kboxYaml := `apiVersion: kbox.dev/v1
kind: MultiApp
metadata:
  name: mystack
spec:
services:
  api:
    image: nginx:alpine
    port: 8080
    replicas: 1
    env:
      SERVICE_NAME: api
  web:
    image: nginx:alpine
    port: 3000
    replicas: 1
    dependsOn:
      - api
    env:
      SERVICE_NAME: web
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	// Clean up
	defer func() {
		client := getK8sClient(t)
		client.AppsV1().Deployments(TestNamespace).Delete(context.Background(), "mystack-api", metav1.DeleteOptions{})
		client.AppsV1().Deployments(TestNamespace).Delete(context.Background(), "mystack-web", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "mystack-api", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "mystack-web", metav1.DeleteOptions{})
		client.CoreV1().ConfigMaps(TestNamespace).Delete(context.Background(), "mystack-api", metav1.DeleteOptions{})
		client.CoreV1().ConfigMaps(TestNamespace).Delete(context.Background(), "mystack-web", metav1.DeleteOptions{})
	}()

	t.Run("deploys all services", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "deploy", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Deploy failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		combined := stdout + stderr
		if !strings.Contains(combined, "Deploying mystack") {
			t.Error("Expected 'Deploying mystack' in output")
		}
	})

	t.Run("creates both deployments", func(t *testing.T) {
		client := getK8sClient(t)

		// Check api deployment
		apiDep, err := client.AppsV1().Deployments(TestNamespace).Get(
			context.Background(), "mystack-api", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("API deployment not found: %v", err)
		}
		if apiDep.Spec.Template.Spec.Containers[0].Image != "nginx:alpine" {
			t.Errorf("Expected nginx:alpine image for api, got %s", apiDep.Spec.Template.Spec.Containers[0].Image)
		}

		// Check web deployment
		webDep, err := client.AppsV1().Deployments(TestNamespace).Get(
			context.Background(), "mystack-web", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Web deployment not found: %v", err)
		}
		if webDep.Spec.Template.Spec.Containers[0].Image != "nginx:alpine" {
			t.Errorf("Expected nginx:alpine image for web, got %s", webDep.Spec.Template.Spec.Containers[0].Image)
		}
	})

	t.Run("creates both services", func(t *testing.T) {
		client := getK8sClient(t)

		// Check api service
		apiSvc, err := client.CoreV1().Services(TestNamespace).Get(
			context.Background(), "mystack-api", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("API service not found: %v", err)
		}
		if len(apiSvc.Spec.Ports) == 0 || apiSvc.Spec.Ports[0].Port != 8080 {
			t.Error("Expected API service on port 8080")
		}

		// Check web service
		webSvc, err := client.CoreV1().Services(TestNamespace).Get(
			context.Background(), "mystack-web", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Web service not found: %v", err)
		}
		if len(webSvc.Spec.Ports) == 0 || webSvc.Spec.Ports[0].Port != 3000 {
			t.Error("Expected web service on port 3000")
		}
	})
}

func TestMultiServiceWithDependsOn(t *testing.T) {
	// Test that dependsOn services get the URL environment variable
	kboxYaml := `apiVersion: kbox.dev/v1
kind: MultiApp
metadata:
  name: depstack
spec:
services:
  db:
    image: postgres:15-alpine
    port: 5432
    replicas: 1
  api:
    image: nginx:alpine
    port: 8080
    replicas: 1
    dependsOn:
      - db
  web:
    image: nginx:alpine
    port: 3000
    replicas: 1
    dependsOn:
      - api
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	// Clean up
	defer func() {
		client := getK8sClient(t)
		client.AppsV1().Deployments(TestNamespace).Delete(context.Background(), "depstack-db", metav1.DeleteOptions{})
		client.AppsV1().Deployments(TestNamespace).Delete(context.Background(), "depstack-api", metav1.DeleteOptions{})
		client.AppsV1().Deployments(TestNamespace).Delete(context.Background(), "depstack-web", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "depstack-db", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "depstack-api", metav1.DeleteOptions{})
		client.CoreV1().Services(TestNamespace).Delete(context.Background(), "depstack-web", metav1.DeleteOptions{})
	}()

	t.Run("deploys services in dependency order", func(t *testing.T) {
		_, _, err := runKbox(t, appDir, "deploy", "--no-wait", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Deploy failed: %v", err)
		}

		client := getK8sClient(t)

		// All services should be created
		_, err = client.AppsV1().Deployments(TestNamespace).Get(context.Background(), "depstack-db", metav1.GetOptions{})
		if err != nil {
			t.Errorf("DB deployment not found: %v", err)
		}
		_, err = client.AppsV1().Deployments(TestNamespace).Get(context.Background(), "depstack-api", metav1.GetOptions{})
		if err != nil {
			t.Errorf("API deployment not found: %v", err)
		}
		_, err = client.AppsV1().Deployments(TestNamespace).Get(context.Background(), "depstack-web", metav1.GetOptions{})
		if err != nil {
			t.Errorf("Web deployment not found: %v", err)
		}
	})

	t.Run("injects service discovery env vars", func(t *testing.T) {
		client := getK8sClient(t)

		// API should have DB_URL
		apiDep, err := client.AppsV1().Deployments(TestNamespace).Get(
			context.Background(), "depstack-api", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("API deployment not found: %v", err)
		}

		foundDBURL := false
		for _, env := range apiDep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "DB_URL" {
				foundDBURL = true
				if !strings.Contains(env.Value, "depstack-db") {
					t.Errorf("Expected DB_URL to contain depstack-db, got %s", env.Value)
				}
			}
		}
		if !foundDBURL {
			t.Error("Expected DB_URL env var in API deployment")
		}

		// Web should have API_URL
		webDep, err := client.AppsV1().Deployments(TestNamespace).Get(
			context.Background(), "depstack-web", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Web deployment not found: %v", err)
		}

		foundAPIURL := false
		for _, env := range webDep.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "API_URL" {
				foundAPIURL = true
				if !strings.Contains(env.Value, "depstack-api") {
					t.Errorf("Expected API_URL to contain depstack-api, got %s", env.Value)
				}
			}
		}
		if !foundAPIURL {
			t.Error("Expected API_URL env var in Web deployment")
		}
	})
}

func TestMultiServiceRender(t *testing.T) {
	kboxYaml := `apiVersion: kbox.dev/v1
kind: MultiApp
metadata:
  name: rendertest
spec:
services:
  api:
    image: api:v1
    port: 8080
  worker:
    image: worker:v1
    port: 9090
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	t.Run("render shows all services", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "render")
		if err != nil {
			t.Fatalf("Render failed: %v\nstderr: %s", err, stderr)
		}

		// Should contain both deployments
		if !strings.Contains(stdout, "rendertest-api") {
			t.Error("Expected rendertest-api in render output")
		}
		if !strings.Contains(stdout, "rendertest-worker") {
			t.Error("Expected rendertest-worker in render output")
		}

		// Should contain both images
		if !strings.Contains(stdout, "api:v1") {
			t.Error("Expected api:v1 image in render output")
		}
		if !strings.Contains(stdout, "worker:v1") {
			t.Error("Expected worker:v1 image in render output")
		}
	})
}

func TestMultiServiceValidation(t *testing.T) {
	t.Run("fails without services", func(t *testing.T) {
		kboxYaml := `apiVersion: kbox.dev/v1
kind: MultiApp
metadata:
  name: emptyapp
spec:
services: {}
`
		appDir := t.TempDir()
		os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

		_, stderr, err := runKbox(t, appDir, "render")
		if err == nil {
			t.Error("Expected error for empty services")
		}
		if !strings.Contains(stderr, "service") {
			t.Errorf("Expected error about services, got: %s", stderr)
		}
	})

	t.Run("fails with circular dependency", func(t *testing.T) {
		kboxYaml := `apiVersion: kbox.dev/v1
kind: MultiApp
metadata:
  name: circular
spec:
services:
  a:
    image: a:v1
    port: 8080
    dependsOn:
      - b
  b:
    image: b:v1
    port: 8080
    dependsOn:
      - a
`
		appDir := t.TempDir()
		os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

		_, stderr, err := runKbox(t, appDir, "render")
		if err == nil {
			t.Error("Expected error for circular dependency")
		}
		if !strings.Contains(stderr, "circular") {
			t.Errorf("Expected circular dependency error, got: %s", stderr)
		}
	})

	t.Run("fails with unknown dependency", func(t *testing.T) {
		kboxYaml := `apiVersion: kbox.dev/v1
kind: MultiApp
metadata:
  name: unknown
spec:
services:
  api:
    image: api:v1
    port: 8080
    dependsOn:
      - nonexistent
`
		appDir := t.TempDir()
		os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

		_, stderr, err := runKbox(t, appDir, "render")
		if err == nil {
			t.Error("Expected error for unknown dependency")
		}
		if !strings.Contains(stderr, "unknown") || !strings.Contains(stderr, "nonexistent") {
			t.Errorf("Expected unknown service error, got: %s", stderr)
		}
	})
}

func TestMultiServiceDryRun(t *testing.T) {
	kboxYaml := `apiVersion: kbox.dev/v1
kind: MultiApp
metadata:
  name: dryruntest
spec:
services:
  api:
    image: api:v1
    port: 8080
`

	appDir := t.TempDir()
	os.WriteFile(filepath.Join(appDir, "kbox.yaml"), []byte(kboxYaml), 0644)

	t.Run("dry-run shows manifests without applying", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, appDir, "deploy", "--dry-run", "-n", TestNamespace)
		if err != nil {
			t.Fatalf("Dry-run failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "dryruntest-api") {
			t.Error("Expected dryruntest-api in dry-run output")
		}
		if !strings.Contains(stdout, "kind: Deployment") {
			t.Error("Expected Deployment in dry-run output")
		}
		if !strings.Contains(stdout, "kind: Service") {
			t.Error("Expected Service in dry-run output")
		}
	})
}
