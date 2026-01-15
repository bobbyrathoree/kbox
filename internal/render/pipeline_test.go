package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bobbyrathoree/kbox/internal/config"
)

// TestFullPipelineExperience tests the complete render flow
// This validates the "it just works" experience
func TestFullPipelineExperience(t *testing.T) {
	t.Run("minimal config produces deployable YAML", func(t *testing.T) {
		// Minimal config - just name and image
		cfg := &config.AppConfig{
			Metadata: config.Metadata{Name: "myapp"},
			Spec: config.AppSpec{
				Image: "myapp:v1",
			},
		}
		cfg.WithDefaults()

		renderer := New(cfg)
		bundle, err := renderer.Render()
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}

		var buf bytes.Buffer
		if err := bundle.ToYAML(&buf); err != nil {
			t.Fatalf("YAML conversion failed: %v", err)
		}

		yaml := buf.String()

		// Must have required K8s fields
		requiredFields := []string{
			"apiVersion:",
			"kind: Deployment",
			"kind: Service",
			"metadata:",
			"spec:",
			"selector:",
			"app: myapp",
		}

		for _, field := range requiredFields {
			if !strings.Contains(yaml, field) {
				t.Errorf("missing required field %q in YAML output", field)
			}
		}
	})

	t.Run("full config renders all features", func(t *testing.T) {
		cfg := &config.AppConfig{
			APIVersion: config.DefaultAPIVersion,
			Kind:       config.DefaultKind,
			Metadata: config.Metadata{
				Name:      "payments",
				Namespace: "prod",
			},
			Spec: config.AppSpec{
				Image:       "payments:v2.0.0",
				Port:        3000,
				Replicas:    5,
				HealthCheck: "/health",
				Env: map[string]string{
					"LOG_LEVEL": "warn",
					"DB_HOST":   "postgres.prod.svc",
				},
				Resources: &config.ResourceConfig{
					Memory: "512Mi",
					CPU:    "250m",
				},
			},
		}

		renderer := New(cfg)
		bundle, err := renderer.Render()
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}

		// Check Deployment
		dep := bundle.Deployment
		if dep == nil {
			t.Fatal("expected deployment")
		}
		if *dep.Spec.Replicas != 5 {
			t.Errorf("expected 5 replicas, got %d", *dep.Spec.Replicas)
		}
		if dep.Namespace != "prod" {
			t.Errorf("expected namespace prod, got %q", dep.Namespace)
		}

		container := dep.Spec.Template.Spec.Containers[0]
		if container.Image != "payments:v2.0.0" {
			t.Errorf("expected image payments:v2.0.0, got %q", container.Image)
		}
		if container.LivenessProbe == nil {
			t.Error("expected liveness probe for health check")
		}
		if container.ReadinessProbe == nil {
			t.Error("expected readiness probe for health check")
		}

		// Check Service
		if len(bundle.Services) != 1 {
			t.Fatalf("expected 1 service, got %d", len(bundle.Services))
		}
		svc := bundle.Services[0]
		if svc.Spec.Ports[0].Port != 3000 {
			t.Errorf("expected service port 3000, got %d", svc.Spec.Ports[0].Port)
		}

		// Check ConfigMap
		if len(bundle.ConfigMaps) != 1 {
			t.Fatalf("expected 1 configmap, got %d", len(bundle.ConfigMaps))
		}
		cm := bundle.ConfigMaps[0]
		if cm.Data["LOG_LEVEL"] != "warn" {
			t.Errorf("expected LOG_LEVEL=warn in configmap")
		}
	})

	t.Run("labels are consistent across resources", func(t *testing.T) {
		cfg := &config.AppConfig{
			Metadata: config.Metadata{Name: "myapp"},
			Spec:     config.AppSpec{Image: "myapp:v1", Env: map[string]string{"A": "B"}},
		}
		cfg.WithDefaults()

		renderer := New(cfg)
		bundle, err := renderer.Render()
		if err != nil {
			t.Fatalf("render failed: %v", err)
		}

		// All resources should have app=myapp label
		if bundle.Deployment.Labels["app"] != "myapp" {
			t.Error("deployment missing app label")
		}
		if bundle.Services[0].Labels["app"] != "myapp" {
			t.Error("service missing app label")
		}
		if bundle.ConfigMaps[0].Labels["app"] != "myapp" {
			t.Error("configmap missing app label")
		}

		// All should be marked as managed by kbox
		if bundle.Deployment.Labels["app.kubernetes.io/managed-by"] != "kbox" {
			t.Error("deployment missing managed-by label")
		}
	})

	t.Run("YAML output is deterministic", func(t *testing.T) {
		cfg := &config.AppConfig{
			Metadata: config.Metadata{Name: "myapp"},
			Spec: config.AppSpec{
				Image: "myapp:v1",
				Env: map[string]string{
					"Z_VAR": "last",
					"A_VAR": "first",
					"M_VAR": "middle",
				},
			},
		}
		cfg.WithDefaults()

		renderer := New(cfg)

		// Render twice
		bundle1, _ := renderer.Render()
		bundle2, _ := renderer.Render()

		var buf1, buf2 bytes.Buffer
		bundle1.ToYAML(&buf1)
		bundle2.ToYAML(&buf2)

		if buf1.String() != buf2.String() {
			t.Error("YAML output should be deterministic (same input = same output)")
		}
	})
}

// TestApplyOrderInBundle tests that objects are ordered correctly for SSA
func TestApplyOrderInBundle(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Env:   map[string]string{"A": "B"},
		},
	}
	cfg.WithDefaults()

	renderer := New(cfg)
	bundle, _ := renderer.Render()

	objects := bundle.AllObjects()

	// Order should be: ServiceAccount -> ConfigMaps -> Services -> Deployments
	// (Namespace would be first if present, ServiceAccount second)
	if len(objects) < 4 {
		t.Fatalf("expected at least 4 objects, got %d", len(objects))
	}

	// First should be ServiceAccount (security requirement)
	if objects[0].GetObjectKind().GroupVersionKind().Kind != "ServiceAccount" {
		t.Errorf("first object should be ServiceAccount, got %s",
			objects[0].GetObjectKind().GroupVersionKind().Kind)
	}

	// Service before Deployment
	foundService := false
	for i, obj := range objects {
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		if kind == "Service" {
			foundService = true
		}
		if kind == "Deployment" && !foundService {
			t.Error("Service should come before Deployment")
		}
		_ = i
	}
}
