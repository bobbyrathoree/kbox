package render

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// RenderPDB creates a PodDisruptionBudget
// Auto-generates when replicas > 1 or autoscaling is enabled (for HA)
// Can be explicitly configured via spec.pdb
func (r *Renderer) RenderPDB() *policyv1.PodDisruptionBudget {
	// Check if we should auto-generate PDB for HA workloads
	autoGenerate := false
	if r.config.Spec.Replicas > 1 {
		autoGenerate = true
	}
	if r.config.Spec.Autoscaling != nil && r.config.Spec.Autoscaling.Enabled {
		autoGenerate = true
	}

	// If explicit config provided, use it
	if r.config.Spec.PDB != nil {
		cfg := r.config.Spec.PDB
		if cfg.MinAvailable != "" || cfg.MaxUnavailable != "" {
			pdb := r.createPDB()
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
	}

	// Auto-generate for HA workloads if no explicit config
	if autoGenerate {
		pdb := r.createPDB()
		// Default: allow max 1 unavailable to ensure availability during updates
		maxUnavail := intstr.FromInt(1)
		pdb.Spec.MaxUnavailable = &maxUnavail
		return pdb
	}

	return nil
}

// createPDB creates the base PDB structure
func (r *Renderer) createPDB() *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
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
}
