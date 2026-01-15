package render

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderHPA creates a HorizontalPodAutoscaler if autoscaling is enabled
func (r *Renderer) RenderHPA() *autoscalingv2.HorizontalPodAutoscaler {
	if r.config.Spec.Autoscaling == nil || !r.config.Spec.Autoscaling.Enabled {
		return nil
	}

	cfg := r.config.Spec.Autoscaling
	minReplicas := int32(cfg.MinReplicas)
	if minReplicas == 0 {
		minReplicas = 1
	}
	maxReplicas := int32(cfg.MaxReplicas)
	if maxReplicas == 0 {
		maxReplicas = 10
	}
	targetCPU := int32(cfg.TargetCPUUtilization)
	if targetCPU == 0 {
		targetCPU = 80
	}

	return &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "autoscaling/v2",
			Kind:       "HorizontalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.config.Metadata.Name,
			Namespace: r.Namespace(),
			Labels:    r.Labels(),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       r.config.Metadata.Name,
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
	}
}
