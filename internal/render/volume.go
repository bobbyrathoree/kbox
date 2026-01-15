package render

import (
	"fmt"

	"github.com/bobbyrathoree/kbox/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RenderVolumes creates PersistentVolumeClaims for volumes that need persistent storage
func (r *Renderer) RenderVolumes() ([]*corev1.PersistentVolumeClaim, error) {
	var pvcs []*corev1.PersistentVolumeClaim

	for _, vol := range r.config.Spec.Volumes {
		// Only create PVCs for volumes with Size specified
		if vol.Size != "" {
			pvc, err := r.renderPVC(vol)
			if err != nil {
				return nil, fmt.Errorf("failed to render PVC for volume %s: %w", vol.Name, err)
			}
			pvcs = append(pvcs, pvc)
		}
	}

	return pvcs, nil
}

// renderPVC creates a PersistentVolumeClaim for a volume
func (r *Renderer) renderPVC(vol config.VolumeConfig) (*corev1.PersistentVolumeClaim, error) {
	quantity, err := resource.ParseQuantity(vol.Size)
	if err != nil {
		return nil, fmt.Errorf("invalid size %q: %w", vol.Size, err)
	}

	pvcName := fmt.Sprintf("%s-%s", r.config.Metadata.Name, vol.Name)

	pvc := &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: r.Namespace(),
			Labels:    r.Labels(),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: quantity,
				},
			},
		},
	}

	return pvc, nil
}

// renderPodVolumes returns Volume specs for the PodSpec
func (r *Renderer) renderPodVolumes() []corev1.Volume {
	var volumes []corev1.Volume

	for _, vol := range r.config.Spec.Volumes {
		volume := corev1.Volume{
			Name: vol.Name,
		}

		switch {
		case vol.Size != "":
			// PVC-backed volume
			pvcName := fmt.Sprintf("%s-%s", r.config.Metadata.Name, vol.Name)
			volume.VolumeSource = corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			}
		case vol.EmptyDir:
			// Ephemeral volume
			volume.VolumeSource = corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			}
		case vol.ConfigMap != "":
			// ConfigMap volume
			volume.VolumeSource = corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: vol.ConfigMap,
					},
				},
			}
		case vol.Secret != "":
			// Secret volume
			volume.VolumeSource = corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: vol.Secret,
				},
			}
		}

		volumes = append(volumes, volume)
	}

	return volumes
}

// renderVolumeMounts returns VolumeMounts for containers
func (r *Renderer) renderVolumeMounts() []corev1.VolumeMount {
	var mounts []corev1.VolumeMount

	for _, vol := range r.config.Spec.Volumes {
		mount := corev1.VolumeMount{
			Name:      vol.Name,
			MountPath: vol.MountPath,
			ReadOnly:  vol.ReadOnly,
		}

		// SubPath is used when mounting a specific key from ConfigMap/Secret
		if vol.SubPath != "" {
			mount.SubPath = vol.SubPath
		}

		mounts = append(mounts, mount)
	}

	return mounts
}
