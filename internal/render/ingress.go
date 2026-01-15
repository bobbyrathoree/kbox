package render

import (
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderIngress creates an Ingress resource from the config
func (r *Renderer) RenderIngress() (*networkingv1.Ingress, error) {
	cfg := r.config.Spec.Ingress
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	// Set default path if not specified
	path := cfg.Path
	if path == "" {
		path = "/"
	}

	// PathType for the ingress rule
	pathType := networkingv1.PathTypePrefix

	// Build ingress rules
	var rules []networkingv1.IngressRule
	if cfg.Host != "" {
		rules = append(rules, networkingv1.IngressRule{
			Host: cfg.Host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     path,
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: r.config.Metadata.Name,
									Port: networkingv1.ServiceBackendPort{
										Number: int32(r.config.Spec.Port),
									},
								},
							},
						},
					},
				},
			},
		})
	} else {
		// No host specified - use wildcard
		rules = append(rules, networkingv1.IngressRule{
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     path,
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: r.config.Metadata.Name,
									Port: networkingv1.ServiceBackendPort{
										Number: int32(r.config.Spec.Port),
									},
								},
							},
						},
					},
				},
			},
		})
	}

	// Build TLS configuration if enabled
	var tls []networkingv1.IngressTLS
	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsConfig := networkingv1.IngressTLS{}
		if cfg.Host != "" {
			tlsConfig.Hosts = []string{cfg.Host}
		}
		if cfg.TLS.SecretName != "" {
			tlsConfig.SecretName = cfg.TLS.SecretName
		} else if cfg.Host != "" {
			// Auto-generate secret name based on host
			tlsConfig.SecretName = r.config.Metadata.Name + "-tls"
		}
		tls = append(tls, tlsConfig)
	}

	// Merge annotations
	annotations := make(map[string]string)
	// Add any user-specified annotations
	for k, v := range cfg.Annotations {
		annotations[k] = v
	}

	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.config.Metadata.Name,
			Namespace:   r.Namespace(),
			Labels:      r.Labels(),
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			Rules: rules,
			TLS:   tls,
		},
	}

	return ingress, nil
}
