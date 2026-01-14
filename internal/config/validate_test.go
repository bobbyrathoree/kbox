package config

import (
	"strings"
	"testing"
)

func TestValidate_ValidConfig(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image:    "myapp:v1",
			Port:     8080,
			Replicas: 1,
		},
	}

	if err := Validate(config); err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{},
		Spec:       AppSpec{Image: "myapp:v1"},
	}

	err := Validate(config)
	if err == nil {
		t.Error("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "metadata.name") {
		t.Errorf("expected error about metadata.name, got: %v", err)
	}
}

func TestValidate_InvalidName(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		wantErr bool
	}{
		{"valid lowercase", "myapp", false},
		{"valid with hyphen", "my-app", false},
		{"valid with numbers", "myapp123", false},
		{"invalid uppercase", "MyApp", true},
		{"invalid start with number", "123app", true},
		{"invalid underscore", "my_app", true},
		{"invalid end with hyphen", "myapp-", true},
		{"empty", "", true},
		{"too long", strings.Repeat("a", 64), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AppConfig{
				APIVersion: DefaultAPIVersion,
				Kind:       DefaultKind,
				Metadata:   Metadata{Name: tt.appName},
				Spec:       AppSpec{Image: "myapp:v1"},
			}

			err := Validate(config)
			hasErr := err != nil
			if hasErr != tt.wantErr {
				t.Errorf("name=%q: wantErr=%v, gotErr=%v (%v)", tt.appName, tt.wantErr, hasErr, err)
			}
		})
	}
}

func TestValidate_MissingImageAndBuild(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec:       AppSpec{},
	}

	err := Validate(config)
	if err == nil {
		t.Error("expected error for missing image and build")
	}
	if !strings.Contains(err.Error(), "image") {
		t.Errorf("expected error about image, got: %v", err)
	}
}

func TestValidate_WithBuild(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec: AppSpec{
			Build: &BuildConfig{
				Context:    ".",
				Dockerfile: "Dockerfile",
			},
		},
	}

	if err := Validate(config); err != nil {
		t.Errorf("expected valid config with build, got error: %v", err)
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image: "myapp:v1",
			Port:  70000, // Invalid port
		},
	}

	err := Validate(config)
	if err == nil {
		t.Error("expected error for invalid port")
	}
}

func TestValidate_InvalidReplicas(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image:    "myapp:v1",
			Replicas: -1,
		},
	}

	err := Validate(config)
	if err == nil {
		t.Error("expected error for negative replicas")
	}
}

func TestValidate_InvalidServiceType(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image: "myapp:v1",
			Service: &ServiceConfig{
				Type: "InvalidType",
			},
		},
	}

	err := Validate(config)
	if err == nil {
		t.Error("expected error for invalid service type")
	}
}

func TestValidate_IngressWithoutHost(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image: "myapp:v1",
			Ingress: &IngressConfig{
				Enabled: true,
				// Host is missing
			},
		},
	}

	err := Validate(config)
	if err == nil {
		t.Error("expected error for ingress without host")
	}
}

func TestValidate_ValidIngress(t *testing.T) {
	config := &AppConfig{
		APIVersion: DefaultAPIVersion,
		Kind:       DefaultKind,
		Metadata:   Metadata{Name: "myapp"},
		Spec: AppSpec{
			Image: "myapp:v1",
			Ingress: &IngressConfig{
				Enabled: true,
				Host:    "myapp.example.com",
			},
		},
	}

	if err := Validate(config); err != nil {
		t.Errorf("expected valid config with ingress, got error: %v", err)
	}
}

func TestIsValidName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"a", true},
		{"abc", true},
		{"a-b-c", true},
		{"a123", true},
		{"abc-123", true},
		{"", false},
		{"A", false},
		{"ABC", false},
		{"1abc", false},
		{"-abc", false},
		{"abc-", false},
		{"a_b", false},
		{"a.b", false},
		{strings.Repeat("a", 63), true},
		{strings.Repeat("a", 64), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidName(tt.name)
			if got != tt.valid {
				t.Errorf("isValidName(%q) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}
