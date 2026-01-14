package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bobbyrathoree/kbox/internal/config"
)

func TestRenderDeployment(t *testing.T) {
	cfg := &config.AppConfig{
		APIVersion: config.DefaultAPIVersion,
		Kind:       config.DefaultKind,
		Metadata: config.Metadata{
			Name: "myapp",
		},
		Spec: config.AppSpec{
			Image:    "myapp:v1.0.0",
			Port:     8080,
			Replicas: 3,
			Env: map[string]string{
				"LOG_LEVEL": "debug",
			},
			HealthCheck: "/health",
			Resources: &config.ResourceConfig{
				Memory: "256Mi",
				CPU:    "100m",
			},
		},
	}

	renderer := New(cfg)
	dep, err := renderer.RenderDeployment()
	if err != nil {
		t.Fatalf("failed to render deployment: %v", err)
	}

	// Check basic fields
	if dep.Name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", dep.Name)
	}

	if *dep.Spec.Replicas != 3 {
		t.Errorf("expected replicas 3, got %d", *dep.Spec.Replicas)
	}

	// Check container
	if len(dep.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(dep.Spec.Template.Spec.Containers))
	}

	container := dep.Spec.Template.Spec.Containers[0]
	if container.Image != "myapp:v1.0.0" {
		t.Errorf("expected image 'myapp:v1.0.0', got %q", container.Image)
	}

	if len(container.Ports) != 1 || container.Ports[0].ContainerPort != 8080 {
		t.Errorf("expected port 8080, got %v", container.Ports)
	}

	// Check probes
	if container.LivenessProbe == nil {
		t.Error("expected liveness probe")
	}
	if container.ReadinessProbe == nil {
		t.Error("expected readiness probe")
	}

	// Check labels
	if dep.Labels["app"] != "myapp" {
		t.Errorf("expected label app=myapp, got %v", dep.Labels)
	}
	if dep.Labels["app.kubernetes.io/managed-by"] != "kbox" {
		t.Errorf("expected managed-by label")
	}
}

func TestRenderService(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Port:  3000,
		},
	}

	renderer := New(cfg)
	svc, err := renderer.RenderService()
	if err != nil {
		t.Fatalf("failed to render service: %v", err)
	}

	if svc.Name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", svc.Name)
	}

	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(svc.Spec.Ports))
	}

	if svc.Spec.Ports[0].Port != 3000 {
		t.Errorf("expected port 3000, got %d", svc.Spec.Ports[0].Port)
	}
}

func TestRenderConfigMap(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Env: map[string]string{
				"DB_HOST":   "localhost",
				"LOG_LEVEL": "debug",
			},
		},
	}

	renderer := New(cfg)
	cm, err := renderer.RenderConfigMap()
	if err != nil {
		t.Fatalf("failed to render configmap: %v", err)
	}

	if cm.Name != "myapp-config" {
		t.Errorf("expected name 'myapp-config', got %q", cm.Name)
	}

	if cm.Data["DB_HOST"] != "localhost" {
		t.Errorf("expected DB_HOST=localhost, got %q", cm.Data["DB_HOST"])
	}

	if cm.Data["LOG_LEVEL"] != "debug" {
		t.Errorf("expected LOG_LEVEL=debug, got %q", cm.Data["LOG_LEVEL"])
	}
}

func TestRenderBundle(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Port:  8080,
			Env: map[string]string{
				"LOG_LEVEL": "info",
			},
		},
	}

	renderer := New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		t.Fatalf("failed to render bundle: %v", err)
	}

	if bundle.Deployment == nil {
		t.Error("expected deployment in bundle")
	}

	if len(bundle.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(bundle.Services))
	}

	if len(bundle.ConfigMaps) != 1 {
		t.Errorf("expected 1 configmap, got %d", len(bundle.ConfigMaps))
	}
}

func TestBundleToYAML(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Port:  8080,
		},
	}

	renderer := New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		t.Fatalf("failed to render bundle: %v", err)
	}

	var buf bytes.Buffer
	if err := bundle.ToYAML(&buf); err != nil {
		t.Fatalf("failed to convert to YAML: %v", err)
	}

	yaml := buf.String()

	// Check for key markers
	if !strings.Contains(yaml, "kind: Service") {
		t.Error("expected Service in YAML output")
	}

	if !strings.Contains(yaml, "kind: Deployment") {
		t.Error("expected Deployment in YAML output")
	}

	if !strings.Contains(yaml, "---") {
		t.Error("expected document separator in YAML output")
	}
}

func TestImageWithTag(t *testing.T) {
	tests := []struct {
		base     string
		tag      string
		expected string
	}{
		{"myapp", "v1.0.0", "myapp:v1.0.0"},
		{"myapp:latest", "v1.0.0", "myapp:v1.0.0"},
		{"registry.io/myapp", "v1", "registry.io/myapp:v1"},
		{"registry.io/myapp:old", "new", "registry.io/myapp:new"},
		{"registry.io:5000/myapp", "v1", "registry.io:5000/myapp:v1"},
	}

	for _, tt := range tests {
		t.Run(tt.base+"_"+tt.tag, func(t *testing.T) {
			result := ImageWithTag(tt.base, tt.tag)
			if result != tt.expected {
				t.Errorf("ImageWithTag(%q, %q) = %q, want %q", tt.base, tt.tag, result, tt.expected)
			}
		})
	}
}
