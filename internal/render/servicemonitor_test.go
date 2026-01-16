package render

import (
	"testing"

	"github.com/bobbyrathoree/kbox/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRenderServiceMonitor_Disabled(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			// No metrics config
		},
	}

	r := New(cfg)
	sm := r.RenderServiceMonitor()

	if sm != nil {
		t.Error("expected nil ServiceMonitor when metrics not configured")
	}
}

func TestRenderServiceMonitor_DisabledExplicitly(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Metrics: &config.MetricsConfig{
				Enabled: false,
			},
		},
	}

	r := New(cfg)
	sm := r.RenderServiceMonitor()

	if sm != nil {
		t.Error("expected nil ServiceMonitor when metrics explicitly disabled")
	}
}

func TestRenderServiceMonitor_Enabled(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{
			Name:      "myapp",
			Namespace: "production",
		},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Metrics: &config.MetricsConfig{
				Enabled: true,
			},
		},
	}

	r := New(cfg)
	sm := r.RenderServiceMonitor()

	if sm == nil {
		t.Fatal("expected non-nil ServiceMonitor")
	}

	// Verify GVK
	apiVersion, _, _ := unstructured.NestedString(sm.Object, "apiVersion")
	if apiVersion != "monitoring.coreos.com/v1" {
		t.Errorf("expected apiVersion 'monitoring.coreos.com/v1', got %q", apiVersion)
	}

	kind, _, _ := unstructured.NestedString(sm.Object, "kind")
	if kind != "ServiceMonitor" {
		t.Errorf("expected kind 'ServiceMonitor', got %q", kind)
	}

	// Verify metadata
	name, _, _ := unstructured.NestedString(sm.Object, "metadata", "name")
	if name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", name)
	}

	namespace, _, _ := unstructured.NestedString(sm.Object, "metadata", "namespace")
	if namespace != "production" {
		t.Errorf("expected namespace 'production', got %q", namespace)
	}

	// Verify default endpoint values
	endpoints, _, _ := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}

	endpoint := endpoints[0].(map[string]interface{})
	if endpoint["path"] != "/metrics" {
		t.Errorf("expected default path '/metrics', got %q", endpoint["path"])
	}
	if endpoint["port"] != "http" {
		t.Errorf("expected default port 'http', got %q", endpoint["port"])
	}
	if endpoint["interval"] != "30s" {
		t.Errorf("expected default interval '30s', got %q", endpoint["interval"])
	}
}

func TestRenderServiceMonitor_CustomConfig(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{
			Name:      "myapp",
			Namespace: "staging",
		},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Metrics: &config.MetricsConfig{
				Enabled:  true,
				Path:     "/custom/metrics",
				Port:     "metrics-port",
				Interval: "15s",
			},
		},
	}

	r := New(cfg)
	sm := r.RenderServiceMonitor()

	if sm == nil {
		t.Fatal("expected non-nil ServiceMonitor")
	}

	// Verify custom endpoint values
	endpoints, _, _ := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}

	endpoint := endpoints[0].(map[string]interface{})
	if endpoint["path"] != "/custom/metrics" {
		t.Errorf("expected custom path '/custom/metrics', got %q", endpoint["path"])
	}
	if endpoint["port"] != "metrics-port" {
		t.Errorf("expected custom port 'metrics-port', got %q", endpoint["port"])
	}
	if endpoint["interval"] != "15s" {
		t.Errorf("expected custom interval '15s', got %q", endpoint["interval"])
	}
}

func TestRenderServiceMonitor_Selector(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Metrics: &config.MetricsConfig{
				Enabled: true,
			},
		},
	}

	r := New(cfg)
	sm := r.RenderServiceMonitor()

	if sm == nil {
		t.Fatal("expected non-nil ServiceMonitor")
	}

	// Verify selector
	matchLabels, _, _ := unstructured.NestedStringMap(sm.Object, "spec", "selector", "matchLabels")
	if matchLabels["app"] != "myapp" {
		t.Errorf("expected selector app=myapp, got %v", matchLabels)
	}
}

func TestRenderServiceMonitor_Labels(t *testing.T) {
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image: "myapp:v1",
			Metrics: &config.MetricsConfig{
				Enabled: true,
			},
		},
	}

	r := New(cfg)
	sm := r.RenderServiceMonitor()

	if sm == nil {
		t.Fatal("expected non-nil ServiceMonitor")
	}

	// Verify labels
	labels, _, _ := unstructured.NestedStringMap(sm.Object, "metadata", "labels")
	if labels["app"] != "myapp" {
		t.Errorf("expected label app=myapp, got %v", labels)
	}
	if labels["app.kubernetes.io/managed-by"] != "kbox" {
		t.Errorf("expected label app.kubernetes.io/managed-by=kbox, got %v", labels)
	}
}

func TestToInterfaceMap(t *testing.T) {
	input := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	result := toInterfaceMap(input)

	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
	if result["key1"] != "value1" {
		t.Errorf("expected key1=value1, got %v", result["key1"])
	}
	if result["key2"] != "value2" {
		t.Errorf("expected key2=value2, got %v", result["key2"])
	}
}
