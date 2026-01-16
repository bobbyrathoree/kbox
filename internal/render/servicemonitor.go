package render

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RenderServiceMonitor creates a Prometheus ServiceMonitor for the app
// Returns nil if metrics are not enabled
func (r *Renderer) RenderServiceMonitor() *unstructured.Unstructured {
	if r.config.Spec.Metrics == nil || !r.config.Spec.Metrics.Enabled {
		return nil
	}

	cfg := r.config.Spec.Metrics

	// Defaults
	path := cfg.Path
	if path == "" {
		path = "/metrics"
	}
	portName := cfg.Port
	if portName == "" {
		portName = "http"
	}
	interval := cfg.Interval
	if interval == "" {
		interval = "30s"
	}

	// Build endpoint spec
	endpoint := map[string]interface{}{
		"port":     portName,
		"path":     path,
		"interval": interval,
	}

	// Build ServiceMonitor using unstructured to avoid prometheus-operator dependency
	sm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "ServiceMonitor",
			"metadata": map[string]interface{}{
				"name":      r.config.Metadata.Name,
				"namespace": r.Namespace(),
				"labels":    toInterfaceMap(r.Labels()),
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": toInterfaceMap(r.Selector()),
				},
				"endpoints": []interface{}{endpoint},
			},
		},
	}

	return sm
}

// toInterfaceMap converts map[string]string to map[string]interface{}
func toInterfaceMap(m map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		result[k] = v
	}
	return result
}
