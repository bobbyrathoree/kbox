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
	result := config.ForEnvironment("")
	if result.Spec.Replicas != 1 {
		t.Errorf("expected replicas 1 for empty env, got %d", result.Spec.Replicas)
	}

	// Test with non-existent environment
	result = config.ForEnvironment("staging")
	if result.Spec.Replicas != 1 {
		t.Errorf("expected replicas 1 for unknown env, got %d", result.Spec.Replicas)
	}

	// Test with prod environment
	result = config.ForEnvironment("prod")
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
