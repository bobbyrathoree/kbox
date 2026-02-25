package config

import (
	"testing"
)

func TestNewDefaultConfig(t *testing.T) {
	config := NewDefaultConfig("myapp")

	if config.APIVersion != DefaultAPIVersion {
		t.Errorf("expected APIVersion %q, got %q", DefaultAPIVersion, config.APIVersion)
	}
	if config.Kind != DefaultKind {
		t.Errorf("expected Kind %q, got %q", DefaultKind, config.Kind)
	}
	if config.Metadata.Name != "myapp" {
		t.Errorf("expected Name %q, got %q", "myapp", config.Metadata.Name)
	}
	if config.Spec.Port != DefaultPort {
		t.Errorf("expected Port %d, got %d", DefaultPort, config.Spec.Port)
	}
	if config.Spec.Replicas != DefaultReplicas {
		t.Errorf("expected Replicas %d, got %d", DefaultReplicas, config.Spec.Replicas)
	}
}

func TestWithDefaults(t *testing.T) {
	config := &AppConfig{
		Metadata: Metadata{Name: "test"},
		Spec:     AppSpec{Image: "test:latest"},
	}

	config.WithDefaults()

	if config.APIVersion != DefaultAPIVersion {
		t.Errorf("expected APIVersion to be set to default")
	}
	if config.Kind != DefaultKind {
		t.Errorf("expected Kind to be set to default")
	}
	if config.Spec.Port != DefaultPort {
		t.Errorf("expected Port to be set to default")
	}
	if config.Spec.Replicas != DefaultReplicas {
		t.Errorf("expected Replicas to be set to default")
	}
}

func TestForEnvironment(t *testing.T) {
	replicas := 5
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image:    "myapp:v1",
			Port:     8080,
			Replicas: 1,
			Env: map[string]string{
				"LOG_LEVEL": "info",
			},
		},
		Environments: map[string]EnvOverride{
			"prod": {
				Replicas: &replicas,
				Env: map[string]string{
					"LOG_LEVEL": "warn",
					"NEW_VAR":   "value",
				},
			},
		},
	}

	// Test with no environment
	result, err := config.ForEnvironment("")
	if err != nil {
		t.Errorf("unexpected error for empty env: %v", err)
	}
	if result.Spec.Replicas != 1 {
		t.Errorf("expected replicas 1 for empty env, got %d", result.Spec.Replicas)
	}

	// Test with non-existent environment - should return error
	_, err = config.ForEnvironment("staging")
	if err == nil {
		t.Errorf("expected error for unknown env 'staging', got nil")
	}

	// Test with prod environment
	result, err = config.ForEnvironment("prod")
	if err != nil {
		t.Errorf("unexpected error for prod env: %v", err)
	}
	if result.Spec.Replicas != 5 {
		t.Errorf("expected replicas 5 for prod, got %d", result.Spec.Replicas)
	}
	if result.Spec.Env["LOG_LEVEL"] != "warn" {
		t.Errorf("expected LOG_LEVEL warn, got %s", result.Spec.Env["LOG_LEVEL"])
	}
	if result.Spec.Env["NEW_VAR"] != "value" {
		t.Errorf("expected NEW_VAR to be added")
	}
}

func TestForEnvironment_DoesNotMutateOriginal(t *testing.T) {
	cfg := &AppConfig{
		Metadata: Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image: "myapp:v1",
			Env:   map[string]string{"KEY": "original"},
		},
		Environments: map[string]EnvOverride{
			"prod": {Env: map[string]string{"KEY": "prod", "NEW": "val"}},
		},
	}

	prodCfg, err := cfg.ForEnvironment("prod")
	if err != nil {
		t.Fatal(err)
	}

	// Verify original is not mutated
	if cfg.Spec.Env["KEY"] != "original" {
		t.Errorf("original env was mutated: KEY=%q", cfg.Spec.Env["KEY"])
	}
	if _, ok := cfg.Spec.Env["NEW"]; ok {
		t.Error("original env has NEW key from prod overlay")
	}

	// Verify prod overlay applied
	if prodCfg.Spec.Env["KEY"] != "prod" {
		t.Errorf("prod env not applied: KEY=%q", prodCfg.Spec.Env["KEY"])
	}
	if prodCfg.Spec.Env["NEW"] != "val" {
		t.Errorf("prod env missing NEW: got %q", prodCfg.Spec.Env["NEW"])
	}
}

func TestForEnvironment_NoEnvironmentsDefined(t *testing.T) {
	config := &AppConfig{
		Metadata: Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image:    "myapp:v1",
			Replicas: 1,
		},
		// No Environments defined
	}

	_, err := config.ForEnvironment("prod")
	if err == nil {
		t.Errorf("expected error when no environments defined, got nil")
	}
}
