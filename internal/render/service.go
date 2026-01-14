package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// RenderService renders a Kubernetes Service from the config
func (r *Renderer) RenderService() (*corev1.Service, error) {
	cfg := r.config
	name := cfg.Metadata.Name

	// Determine service type
	serviceType := corev1.ServiceTypeClusterIP
	if cfg.Spec.Service != nil && cfg.Spec.Service.Type != "" {
		switch cfg.Spec.Service.Type {
		case "ClusterIP":
			serviceType = corev1.ServiceTypeClusterIP
		case "NodePort":
			serviceType = corev1.ServiceTypeNodePort
		case "LoadBalancer":
			serviceType = corev1.ServiceTypeLoadBalancer
		}
	}

	// Determine ports
	port := int32(cfg.Spec.Port)
	targetPort := port
	if cfg.Spec.Service != nil {
		if cfg.Spec.Service.Port != 0 {
			port = int32(cfg.Spec.Service.Port)
		}
		if cfg.Spec.Service.TargetPort != 0 {
			targetPort = int32(cfg.Spec.Service.TargetPort)
		}
	}

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.Namespace(),
			Labels:    r.Labels(),
		},
		Spec: corev1.ServiceSpec{
			Type:     serviceType,
			Selector: r.Selector(),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromInt32(targetPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	return service, nil
}
