package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestZeroConfigExperience tests the "Bun moment" - working with just a Dockerfile
func TestZeroConfigExperience(t *testing.T) {
	t.Run("infers app name from directory", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "myapp")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(tmpDir)

		// Rename to have a specific name
		newDir := filepath.Join(filepath.Dir(tmpDir), "payments-service")
		os.Rename(tmpDir, newDir)
		defer os.RemoveAll(newDir)

		dockerfile := "FROM node:18\nEXPOSE 3000\nCMD [\"node\", \"server.js\"]"
		os.WriteFile(filepath.Join(newDir, "Dockerfile"), []byte(dockerfile), 0644)

		cfg, err := InferFromDockerfile(newDir)
		if err != nil {
			t.Fatalf("failed to infer: %v", err)
		}

		if cfg.Metadata.Name != "payments-service" {
			t.Errorf("expected name 'payments-service', got %q", cfg.Metadata.Name)
		}
	})

	t.Run("infers port from EXPOSE directive", func(t *testing.T) {
		tmpDir := t.TempDir()

		tests := []struct {
			dockerfile string
			wantPort   int
		}{
			{"FROM nginx\nEXPOSE 8080", 8080},
			{"FROM nginx\nEXPOSE 3000/tcp", 3000},
			{"FROM nginx\nexpose 9000", 9000}, // lowercase
			{"FROM nginx", DefaultPort},       // no EXPOSE = default
		}

		for _, tt := range tests {
			os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(tt.dockerfile), 0644)

			cfg, err := InferFromDockerfile(tmpDir)
			if err != nil {
				t.Fatalf("failed to infer: %v", err)
			}

			if cfg.Spec.Port != tt.wantPort {
				t.Errorf("dockerfile %q: expected port %d, got %d", tt.dockerfile, tt.wantPort, cfg.Spec.Port)
			}
		}
	})

	t.Run("sets sensible defaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM alpine"), 0644)

		cfg, err := InferFromDockerfile(tmpDir)
		if err != nil {
			t.Fatalf("failed to infer: %v", err)
		}

		// Should have build config
		if cfg.Spec.Build == nil {
			t.Error("expected build config to be set")
		}
		if cfg.Spec.Build.Dockerfile != "Dockerfile" {
			t.Errorf("expected Dockerfile, got %q", cfg.Spec.Build.Dockerfile)
		}

		// Should have default replicas
		if cfg.Spec.Replicas != DefaultReplicas {
			t.Errorf("expected replicas %d, got %d", DefaultReplicas, cfg.Spec.Replicas)
		}
	})

	t.Run("works without kbox.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM golang:1.21\nEXPOSE 8080"), 0644)

		// No kbox.yaml exists
		loader := NewLoader(tmpDir)
		if loader.HasConfigFile() {
			t.Error("should not have config file")
		}

		// Should still be able to infer
		cfg, err := InferFromDockerfile(tmpDir)
		if err != nil {
			t.Fatalf("zero-config should work: %v", err)
		}

		if cfg.Spec.Port != 8080 {
			t.Errorf("expected inferred port 8080")
		}
	})
}

// TestConfigOverridesPrecedence tests that environment overlays work correctly
func TestConfigOverridesPrecedence(t *testing.T) {
	t.Run("environment overrides base config", func(t *testing.T) {
		prodReplicas := 10
		cfg := &AppConfig{
			Metadata: Metadata{Name: "myapp"},
			Spec: AppSpec{
				Image:    "myapp:v1",
				Replicas: 1,
				Env: map[string]string{
					"LOG_LEVEL": "info",
					"DEBUG":     "false",
				},
			},
			Environments: map[string]EnvOverride{
				"prod": {
					Replicas: &prodReplicas,
					Env: map[string]string{
						"LOG_LEVEL": "warn", // Override
						"SENTRY":    "true", // Add new
					},
				},
			},
		}

		result := cfg.ForEnvironment("prod")

		// Replicas should be overridden
		if result.Spec.Replicas != 10 {
			t.Errorf("expected replicas 10, got %d", result.Spec.Replicas)
		}

		// LOG_LEVEL should be overridden
		if result.Spec.Env["LOG_LEVEL"] != "warn" {
			t.Errorf("expected LOG_LEVEL=warn, got %q", result.Spec.Env["LOG_LEVEL"])
		}

		// DEBUG should be preserved from base
		if result.Spec.Env["DEBUG"] != "false" {
			t.Errorf("expected DEBUG=false preserved, got %q", result.Spec.Env["DEBUG"])
		}

		// SENTRY should be added
		if result.Spec.Env["SENTRY"] != "true" {
			t.Errorf("expected SENTRY=true added, got %q", result.Spec.Env["SENTRY"])
		}
	})

	t.Run("unknown environment returns base config", func(t *testing.T) {
		cfg := &AppConfig{
			Metadata: Metadata{Name: "myapp"},
			Spec: AppSpec{
				Image:    "myapp:v1",
				Replicas: 3,
			},
		}

		result := cfg.ForEnvironment("nonexistent")

		if result.Spec.Replicas != 3 {
			t.Errorf("expected base replicas 3, got %d", result.Spec.Replicas)
		}
	})
}
