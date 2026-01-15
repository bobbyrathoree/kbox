package render

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// RenderPDB creates a PodDisruptionBudget if pdb is configured
func (r *Renderer) RenderPDB() *policyv1.PodDisruptionBudget {
	if r.config.Spec.PDB == nil {
		return nil
	}

	cfg := r.config.Spec.PDB
	if cfg.MinAvailable == "" && cfg.MaxUnavailable == "" {
		return nil
	}

	pdb := &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "policy/v1",
			Kind:       "PodDisruptionBudget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.config.Metadata.Name,
			Namespace: r.Namespace(),
			Labels:    r.Labels(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": r.config.Metadata.Name,
				},
			},
		},
	}

	if cfg.MinAvailable != "" {
		minAvail := intstr.Parse(cfg.MinAvailable)
		pdb.Spec.MinAvailable = &minAvail
	}
	if cfg.MaxUnavailable != "" {
		maxUnavail := intstr.Parse(cfg.MaxUnavailable)
		pdb.Spec.MaxUnavailable = &maxUnavail
	}

	return pdb
}
