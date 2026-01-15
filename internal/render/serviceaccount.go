package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderServiceAccount creates a ServiceAccount for the app
func (r *Renderer) RenderServiceAccount() *corev1.ServiceAccount {
	automount := false
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.config.Metadata.Name,
			Namespace: r.Namespace(),
			Labels:    r.Labels(),
		},
		AutomountServiceAccountToken: &automount,
	}
}
