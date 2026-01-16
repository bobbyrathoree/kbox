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
			got := IsValidName(tt.name)
			if got != tt.valid {
				t.Errorf("IsValidName(%q) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

// Tests for Issue #9: K8s quantity validation
func TestValidate_InvalidQuantity(t *testing.T) {
	tests := []struct {
		name        string
		memory      string
		cpu         string
		memoryLimit string
		cpuLimit    string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid memory",
			memory:  "256Mi",
			wantErr: false,
		},
		{
			name:    "valid cpu",
			cpu:     "100m",
			wantErr: false,
		},
		{
			name:        "invalid memory unit",
			memory:      "500z",
			wantErr:     true,
			errContains: "invalid Kubernetes quantity",
		},
		{
			name:        "invalid cpu format",
			cpu:         "abc",
			wantErr:     true,
			errContains: "invalid Kubernetes quantity",
		},
		{
			name:        "invalid memory limit",
			memoryLimit: "not-a-quantity",
			wantErr:     true,
			errContains: "invalid Kubernetes quantity",
		},
		{
			name:     "invalid cpu limit",
			cpuLimit: "xyz123",
			wantErr:  true,
			errContains: "invalid Kubernetes quantity",
		},
		{
			name:        "valid quantities",
			memory:      "128Mi",
			cpu:         "50m",
			memoryLimit: "256Mi",
			cpuLimit:    "100m",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AppConfig{
				APIVersion: DefaultAPIVersion,
				Kind:       DefaultKind,
				Metadata:   Metadata{Name: "myapp"},
				Spec: AppSpec{
					Image: "myapp:v1",
					Resources: &ResourceConfig{
						Memory:      tt.memory,
						CPU:         tt.cpu,
						MemoryLimit: tt.memoryLimit,
						CPULimit:    tt.cpuLimit,
					},
				},
			}

			err := Validate(config)
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Errorf("wantErr=%v, gotErr=%v (%v)", tt.wantErr, hasErr, err)
			}

			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
			}
		})
	}
}

// Tests for Issue #10: Request <= Limit validation
func TestValidate_RequestExceedsLimit(t *testing.T) {
	tests := []struct {
		name        string
		memory      string
		memoryLimit string
		cpu         string
		cpuLimit    string
		wantErr     bool
		errContains string
	}{
		{
			name:        "memory request equals limit",
			memory:      "256Mi",
			memoryLimit: "256Mi",
			wantErr:     false,
		},
		{
			name:        "memory request less than limit",
			memory:      "128Mi",
			memoryLimit: "256Mi",
			wantErr:     false,
		},
		{
			name:        "memory request exceeds limit",
			memory:      "512Mi",
			memoryLimit: "128Mi",
			wantErr:     true,
			errContains: "memory request (512Mi) exceeds limit (128Mi)",
		},
		{
			name:     "cpu request equals limit",
			cpu:      "100m",
			cpuLimit: "100m",
			wantErr:  false,
		},
		{
			name:     "cpu request less than limit",
			cpu:      "50m",
			cpuLimit: "100m",
			wantErr:  false,
		},
		{
			name:        "cpu request exceeds limit",
			cpu:         "500m",
			cpuLimit:    "100m",
			wantErr:     true,
			errContains: "cpu request (500m) exceeds limit (100m)",
		},
		{
			name:   "only request set (no limit) - valid",
			memory: "256Mi",
			cpu:    "100m",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AppConfig{
				APIVersion: DefaultAPIVersion,
				Kind:       DefaultKind,
				Metadata:   Metadata{Name: "myapp"},
				Spec: AppSpec{
					Image: "myapp:v1",
					Resources: &ResourceConfig{
						Memory:      tt.memory,
						CPU:         tt.cpu,
						MemoryLimit: tt.memoryLimit,
						CPULimit:    tt.cpuLimit,
					},
				},
			}

			err := Validate(config)
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Errorf("wantErr=%v, gotErr=%v (%v)", tt.wantErr, hasErr, err)
			}

			if tt.wantErr && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
			}
		})
	}
}
