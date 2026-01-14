//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportBasicDeployment(t *testing.T) {
	// Create a basic K8s deployment YAML
	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-import-app
  namespace: default
spec:
  replicas: 3
  selector:
    matchLabels:
      app: test-import-app
  template:
    metadata:
      labels:
        app: test-import-app
    spec:
      containers:
      - name: main
        image: nginx:1.21
        ports:
        - containerPort: 80
        env:
        - name: LOG_LEVEL
          value: info
        - name: DEBUG
          value: "true"
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "128Mi"
            cpu: "200m"
        livenessProbe:
          httpGet:
            path: /health
            port: 80
`

	tmpDir := t.TempDir()
	deploymentFile := filepath.Join(tmpDir, "deployment.yaml")
	os.WriteFile(deploymentFile, []byte(deploymentYAML), 0644)

	t.Run("import single deployment file", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, tmpDir, "import", "--ci", deploymentFile)
		if err != nil {
			t.Fatalf("Import failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		// Verify output contains expected kbox.yaml content
		if !strings.Contains(stdout, "apiVersion: kbox.dev/v1") {
			t.Error("Expected apiVersion: kbox.dev/v1 in output")
		}
		if !strings.Contains(stdout, "name: test-import-app") {
			t.Error("Expected name: test-import-app in output")
		}
		if !strings.Contains(stdout, "image: nginx:1.21") {
			t.Error("Expected image: nginx:1.21 in output")
		}
		if !strings.Contains(stdout, "replicas: 3") {
			t.Error("Expected replicas: 3 in output")
		}
		if !strings.Contains(stdout, "port: 80") {
			t.Error("Expected port: 80 in output")
		}
		if !strings.Contains(stdout, "LOG_LEVEL") {
			t.Error("Expected LOG_LEVEL env var in output")
		}
		if !strings.Contains(stdout, "healthCheck:") {
			t.Error("Expected healthCheck in output")
		}
	})
}

func TestImportDeploymentWithService(t *testing.T) {
	// Create deployment and service YAML
	manifestsYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
spec:
  replicas: 2
  selector:
    matchLabels:
      app: webapp
  template:
    metadata:
      labels:
        app: webapp
    spec:
      containers:
      - name: app
        image: myregistry/webapp:v1.2.3
        ports:
        - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: webapp
spec:
  type: LoadBalancer
  selector:
    app: webapp
  ports:
  - port: 80
    targetPort: 8080
`

	tmpDir := t.TempDir()
	manifestFile := filepath.Join(tmpDir, "manifests.yaml")
	os.WriteFile(manifestFile, []byte(manifestsYAML), 0644)

	t.Run("import deployment with service extracts service type", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, tmpDir, "import", "--ci", manifestFile)
		if err != nil {
			t.Fatalf("Import failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "name: webapp") {
			t.Error("Expected name: webapp in output")
		}
		if !strings.Contains(stdout, "image: myregistry/webapp:v1.2.3") {
			t.Error("Expected image in output")
		}
		if !strings.Contains(stdout, "LoadBalancer") {
			t.Error("Expected LoadBalancer service type in output")
		}
	})
}

func TestImportDeploymentWithConfigMap(t *testing.T) {
	// Create deployment and configmap
	manifestsYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
      - name: main
        image: myapp:latest
        ports:
        - containerPort: 3000
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: myapp-config
data:
  DATABASE_URL: postgres://localhost:5432/db
  REDIS_URL: redis://localhost:6379
`

	tmpDir := t.TempDir()
	manifestFile := filepath.Join(tmpDir, "all.yaml")
	os.WriteFile(manifestFile, []byte(manifestsYAML), 0644)

	t.Run("import merges configmap data into env", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, tmpDir, "import", "--ci", manifestFile)
		if err != nil {
			t.Fatalf("Import failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "DATABASE_URL") {
			t.Error("Expected DATABASE_URL from configmap in output")
		}
		if !strings.Contains(stdout, "REDIS_URL") {
			t.Error("Expected REDIS_URL from configmap in output")
		}
	})
}

func TestImportFromDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create deployment.yaml
	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: dir-test-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: dir-test-app
  template:
    metadata:
      labels:
        app: dir-test-app
    spec:
      containers:
      - name: main
        image: nginx:alpine
        ports:
        - containerPort: 80
`

	// Create service.yaml
	serviceYAML := `apiVersion: v1
kind: Service
metadata:
  name: dir-test-app
spec:
  type: NodePort
  selector:
    app: dir-test-app
  ports:
  - port: 80
    targetPort: 80
`

	os.WriteFile(filepath.Join(tmpDir, "deployment.yaml"), []byte(deploymentYAML), 0644)
	os.WriteFile(filepath.Join(tmpDir, "service.yaml"), []byte(serviceYAML), 0644)

	t.Run("import from directory with -f flag", func(t *testing.T) {
		workDir := t.TempDir() // Different working directory
		stdout, stderr, err := runKbox(t, workDir, "import", "--ci", "-f", tmpDir)
		if err != nil {
			t.Fatalf("Import failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "name: dir-test-app") {
			t.Error("Expected name: dir-test-app in output")
		}
		if !strings.Contains(stdout, "NodePort") {
			t.Error("Expected NodePort service type in output")
		}
	})

	t.Run("import directory as positional argument", func(t *testing.T) {
		workDir := t.TempDir()
		stdout, stderr, err := runKbox(t, workDir, "import", "--ci", tmpDir)
		if err != nil {
			t.Fatalf("Import failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "name: dir-test-app") {
			t.Error("Expected app name in output")
		}
	})
}

func TestImportOutputToFile(t *testing.T) {
	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: output-test
spec:
  replicas: 1
  selector:
    matchLabels:
      app: output-test
  template:
    metadata:
      labels:
        app: output-test
    spec:
      containers:
      - name: main
        image: busybox:latest
        ports:
        - containerPort: 8080
`

	tmpDir := t.TempDir()
	deploymentFile := filepath.Join(tmpDir, "deployment.yaml")
	outputFile := filepath.Join(tmpDir, "kbox.yaml")
	os.WriteFile(deploymentFile, []byte(deploymentYAML), 0644)

	t.Run("import with -o writes to file", func(t *testing.T) {
		_, stderr, err := runKbox(t, tmpDir, "import", "--ci", "-o", outputFile, deploymentFile)
		if err != nil {
			t.Fatalf("Import failed: %v\nstderr: %s", err, stderr)
		}

		// Verify file was created
		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatalf("Failed to read output file: %v", err)
		}

		if !strings.Contains(string(content), "apiVersion: kbox.dev/v1") {
			t.Error("Expected kbox.yaml content in output file")
		}
		if !strings.Contains(string(content), "name: output-test") {
			t.Error("Expected app name in output file")
		}
	})
}

func TestImportErrors(t *testing.T) {
	t.Run("fails with no deployment", func(t *testing.T) {
		// Create a manifest with only a Service (no Deployment)
		serviceOnlyYAML := `apiVersion: v1
kind: Service
metadata:
  name: orphan-service
spec:
  selector:
    app: nonexistent
  ports:
  - port: 80
`
		tmpDir := t.TempDir()
		serviceFile := filepath.Join(tmpDir, "service.yaml")
		os.WriteFile(serviceFile, []byte(serviceOnlyYAML), 0644)

		_, stderr, err := runKbox(t, tmpDir, "import", "--ci", serviceFile)
		if err == nil {
			t.Error("Expected error when no Deployment present")
		}

		combined := stderr
		if !strings.Contains(combined, "Deployment") {
			t.Errorf("Expected error about missing Deployment, got: %s", combined)
		}
	})

	t.Run("fails with no input files", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, stderr, err := runKbox(t, tmpDir, "import", "--ci")
		if err == nil {
			t.Error("Expected error when no input files specified")
		}

		if !strings.Contains(stderr, "no input") {
			t.Errorf("Expected error about no input files, got: %s", stderr)
		}
	})

	t.Run("fails with nonexistent file", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, stderr, err := runKbox(t, tmpDir, "import", "--ci", "/nonexistent/file.yaml")
		if err == nil {
			t.Error("Expected error for nonexistent file")
		}

		if !strings.Contains(stderr, "cannot access") {
			t.Errorf("Expected 'cannot access' error, got: %s", stderr)
		}
	})
}

func TestImportWithNamespace(t *testing.T) {
	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: ns-test-app
  namespace: production
spec:
  replicas: 1
  selector:
    matchLabels:
      app: ns-test-app
  template:
    metadata:
      labels:
        app: ns-test-app
    spec:
      containers:
      - name: main
        image: app:v1
        ports:
        - containerPort: 8080
`

	tmpDir := t.TempDir()
	deploymentFile := filepath.Join(tmpDir, "deployment.yaml")
	os.WriteFile(deploymentFile, []byte(deploymentYAML), 0644)

	t.Run("preserves non-default namespace", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, tmpDir, "import", "--ci", deploymentFile)
		if err != nil {
			t.Fatalf("Import failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "namespace: production") {
			t.Error("Expected namespace: production in output")
		}
	})
}

func TestImportWithCommandAndArgs(t *testing.T) {
	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: cmd-test-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cmd-test-app
  template:
    metadata:
      labels:
        app: cmd-test-app
    spec:
      containers:
      - name: main
        image: alpine:latest
        command: ["/bin/sh", "-c"]
        args: ["echo hello && sleep infinity"]
        ports:
        - containerPort: 8080
`

	tmpDir := t.TempDir()
	deploymentFile := filepath.Join(tmpDir, "deployment.yaml")
	os.WriteFile(deploymentFile, []byte(deploymentYAML), 0644)

	t.Run("imports command and args", func(t *testing.T) {
		stdout, stderr, err := runKbox(t, tmpDir, "import", "--ci", deploymentFile)
		if err != nil {
			t.Fatalf("Import failed: %v\nstderr: %s", err, stderr)
		}

		if !strings.Contains(stdout, "command:") {
			t.Error("Expected command in output")
		}
		if !strings.Contains(stdout, "args:") {
			t.Error("Expected args in output")
		}
	})
}
