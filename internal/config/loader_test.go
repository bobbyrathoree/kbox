package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_LoadFile(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "kbox-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write test config
	configContent := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: testapp
spec:
  image: testapp:v1
  port: 3000
  replicas: 2
  env:
    LOG_LEVEL: debug
environments:
  prod:
    replicas: 5
`
	configPath := filepath.Join(tmpDir, "kbox.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(tmpDir)
	config, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if config.Metadata.Name != "testapp" {
		t.Errorf("expected name 'testapp', got %q", config.Metadata.Name)
	}
	if config.Spec.Port != 3000 {
		t.Errorf("expected port 3000, got %d", config.Spec.Port)
	}
	if config.Spec.Replicas != 2 {
		t.Errorf("expected replicas 2, got %d", config.Spec.Replicas)
	}
	if config.Spec.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("expected LOG_LEVEL debug, got %q", config.Spec.Env["LOG_LEVEL"])
	}

	// Check prod environment
	prodConfig := config.ForEnvironment("prod")
	if prodConfig.Spec.Replicas != 5 {
		t.Errorf("expected prod replicas 5, got %d", prodConfig.Spec.Replicas)
	}
}

func TestLoader_LoadYmlExtension(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: testapp
spec:
  image: testapp:v1
`
	configPath := filepath.Join(tmpDir, "kbox.yml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(tmpDir)
	config, err := loader.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if config.Metadata.Name != "testapp" {
		t.Errorf("expected name 'testapp', got %q", config.Metadata.Name)
	}
}

func TestLoader_NoConfigFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	loader := NewLoader(tmpDir)
	_, err = loader.Load()
	if err == nil {
		t.Error("expected error for missing config file")
	}
}

func TestLoader_HasConfigFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	loader := NewLoader(tmpDir)
	if loader.HasConfigFile() {
		t.Error("expected HasConfigFile to be false")
	}

	configPath := filepath.Join(tmpDir, "kbox.yaml")
	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	if !loader.HasConfigFile() {
		t.Error("expected HasConfigFile to be true")
	}
}

func TestInferFromDockerfile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-test-myapp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create Dockerfile with EXPOSE
	dockerfile := `FROM golang:1.21
WORKDIR /app
COPY . .
RUN go build -o main .
EXPOSE 3000
CMD ["./main"]
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := InferFromDockerfile(tmpDir)
	if err != nil {
		t.Fatalf("failed to infer from Dockerfile: %v", err)
	}

	if config.Spec.Port != 3000 {
		t.Errorf("expected port 3000 from EXPOSE, got %d", config.Spec.Port)
	}
	if config.Spec.Build == nil {
		t.Error("expected build config to be set")
	}
}

func TestInferFromDockerfile_NoExpose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfile := `FROM golang:1.21
WORKDIR /app
CMD ["./main"]
`
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		t.Fatal(err)
	}

	config, err := InferFromDockerfile(tmpDir)
	if err != nil {
		t.Fatalf("failed to infer from Dockerfile: %v", err)
	}

	if config.Spec.Port != DefaultPort {
		t.Errorf("expected default port %d, got %d", DefaultPort, config.Spec.Port)
	}
}

func TestInferFromDockerfile_NoDockerfile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = InferFromDockerfile(tmpDir)
	if err == nil {
		t.Error("expected error for missing Dockerfile")
	}
}

func TestParseExposedPort(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		want       int
	}{
		{
			name:       "simple expose",
			dockerfile: "FROM alpine\nEXPOSE 8080\nCMD [\"./app\"]",
			want:       8080,
		},
		{
			name:       "expose with tcp",
			dockerfile: "FROM alpine\nEXPOSE 3000/tcp\nCMD [\"./app\"]",
			want:       3000,
		},
		{
			name:       "lowercase expose",
			dockerfile: "FROM alpine\nexpose 9000\nCMD [\"./app\"]",
			want:       9000,
		},
		{
			name:       "no expose",
			dockerfile: "FROM alpine\nCMD [\"./app\"]",
			want:       0,
		},
		{
			name:       "multiple exposes",
			dockerfile: "FROM alpine\nEXPOSE 8080\nEXPOSE 9000\nCMD [\"./app\"]",
			want:       8080, // First one wins
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExposedPort(tt.dockerfile)
			if got != tt.want {
				t.Errorf("parseExposedPort() = %d, want %d", got, tt.want)
			}
		})
	}
}
