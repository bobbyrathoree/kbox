package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderConfigMap renders a Kubernetes ConfigMap from the config
func (r *Renderer) RenderConfigMap() (*corev1.ConfigMap, error) {
	cfg := r.config
	name := cfg.Metadata.Name + "-config"

	// Copy env vars to ConfigMap data
	data := make(map[string]string)
	for k, v := range cfg.Spec.Env {
		data[k] = v
	}

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.Namespace(),
			Labels:    r.Labels(),
		},
		Data: data,
	}

	return cm, nil
}
