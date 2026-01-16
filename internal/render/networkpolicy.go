package render

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// RenderNetworkPolicy creates a NetworkPolicy for the app
func (r *Renderer) RenderNetworkPolicy() *networkingv1.NetworkPolicy {
	// Protocol and port definitions
	dnsPort := intstr.FromInt(53)
	httpPort := intstr.FromInt(80)
	httpsPort := intstr.FromInt(443)
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP

	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.config.Metadata.Name,
			Namespace: r.Namespace(),
			Labels:    r.Labels(),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": r.config.Metadata.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					// Allow from same app
					From: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": r.config.Metadata.Name,
								},
							},
						},
					},
				},
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					// Allow to dependencies
					To: []networkingv1.NetworkPolicyPeer{
						{
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kbox.dev/app": r.config.Metadata.Name,
								},
							},
						},
					},
				},
				{
					// Allow DNS
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{},
							PodSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"k8s-app": "kube-dns",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: &udp,
							Port:     &dnsPort,
						},
					},
				},
				{
					// Allow external HTTPS/HTTP egress (for APIs, webhooks, etc.)
					// Empty To[] means all external destinations
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: &tcp,
							Port:     &httpsPort,
						},
						{
							Protocol: &tcp,
							Port:     &httpPort,
						},
					},
				},
			},
		},
	}
}
