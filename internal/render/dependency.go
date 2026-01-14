package render

import (
	"fmt"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/dependencies"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DependencyResources holds rendered resources for a dependency
type DependencyResources struct {
	StatefulSet *appsv1.StatefulSet
	Service     *corev1.Service
	Secret      *corev1.Secret
	PVC         *corev1.PersistentVolumeClaim
	EnvVars     map[string]string // To inject into app
}

// RenderDependency renders a single dependency into K8s resources
func (r *Renderer) RenderDependency(dep config.DependencyConfig) (*DependencyResources, error) {
	template, ok := dependencies.Get(dep.Type)
	if !ok {
		return nil, fmt.Errorf("unsupported dependency type: %s\n  → Supported: %v", dep.Type, dependencies.SupportedTypes())
	}

	// Generate service name
	serviceName := fmt.Sprintf("%s-%s", r.config.Metadata.Name, dep.Type)
	namespace := r.Namespace()

	// Generate password if needed
	password := ""
	if len(template.SecretKeys) > 0 {
		password = dependencies.GeneratePassword()
	}

	resources := &DependencyResources{
		EnvVars: dependencies.RenderEnvVars(template, serviceName, password),
	}

	// Labels
	labels := map[string]string{
		"app":                          serviceName,
		"app.kubernetes.io/name":       serviceName,
		"app.kubernetes.io/managed-by": "kbox",
		"kbox.dev/dependency":          dep.Type,
		"kbox.dev/app":                 r.config.Metadata.Name,
	}

	// Create Secret if password is needed
	if password != "" {
		resources.Secret = &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: namespace,
				Labels:    labels,
			},
			StringData: make(map[string]string),
		}
		for _, key := range template.SecretKeys {
			resources.Secret.StringData[key] = password
		}
	}

	// Create Service
	resources.Service = &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": serviceName},
			Ports: []corev1.ServicePort{
				{
					Port:       template.DefaultPort,
					TargetPort: intstr.FromInt(int(template.DefaultPort)),
				},
			},
			ClusterIP: "None", // Headless service for StatefulSet
		},
	}

	// Determine storage size
	storage := dep.Storage
	if storage == "" {
		storage = template.DefaultStorage
	}

	// Create StatefulSet
	image := dependencies.ImageWithVersion(template, dep.Version)
	replicas := int32(1)

	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: serviceName,
			Replicas:    &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": serviceName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  dep.Type,
							Image: image,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: template.DefaultPort,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: getDataPath(dep.Type),
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "data",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							corev1.ReadWriteOnce,
						},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(storage),
							},
						},
					},
				},
			},
		},
	}

	// Add environment from secret if exists
	if resources.Secret != nil {
		for _, key := range template.SecretKeys {
			statefulSet.Spec.Template.Spec.Containers[0].Env = append(
				statefulSet.Spec.Template.Spec.Containers[0].Env,
				corev1.EnvVar{
					Name: key,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: serviceName,
							},
							Key: key,
						},
					},
				},
			)
		}
	}

	// Add readiness probe
	if len(template.HealthCheck) > 0 {
		statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: template.HealthCheck,
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		}
	}

	// Add resource limits if specified
	if dep.Resources != nil {
		resources := corev1.ResourceRequirements{}
		if dep.Resources.Memory != "" {
			if resources.Requests == nil {
				resources.Requests = corev1.ResourceList{}
			}
			resources.Requests[corev1.ResourceMemory] = resource.MustParse(dep.Resources.Memory)
		}
		if dep.Resources.CPU != "" {
			if resources.Requests == nil {
				resources.Requests = corev1.ResourceList{}
			}
			resources.Requests[corev1.ResourceCPU] = resource.MustParse(dep.Resources.CPU)
		}
		if dep.Resources.MemoryLimit != "" {
			if resources.Limits == nil {
				resources.Limits = corev1.ResourceList{}
			}
			resources.Limits[corev1.ResourceMemory] = resource.MustParse(dep.Resources.MemoryLimit)
		}
		if dep.Resources.CPULimit != "" {
			if resources.Limits == nil {
				resources.Limits = corev1.ResourceList{}
			}
			resources.Limits[corev1.ResourceCPU] = resource.MustParse(dep.Resources.CPULimit)
		}
		statefulSet.Spec.Template.Spec.Containers[0].Resources = resources
	}

	resources.StatefulSet = statefulSet

	return resources, nil
}

// RenderAllDependencies renders all dependencies and returns collected resources
func (r *Renderer) RenderAllDependencies() ([]*appsv1.StatefulSet, []*corev1.Service, []*corev1.Secret, map[string]string, error) {
	var statefulSets []*appsv1.StatefulSet
	var services []*corev1.Service
	var secrets []*corev1.Secret
	envVars := make(map[string]string)

	for _, dep := range r.config.Spec.Dependencies {
		res, err := r.RenderDependency(dep)
		if err != nil {
			return nil, nil, nil, nil, err
		}

		statefulSets = append(statefulSets, res.StatefulSet)
		services = append(services, res.Service)
		if res.Secret != nil {
			secrets = append(secrets, res.Secret)
		}

		// Collect env vars to inject into app
		for k, v := range res.EnvVars {
			envVars[k] = v
		}
	}

	return statefulSets, services, secrets, envVars, nil
}

// getDataPath returns the data directory path for a dependency type
func getDataPath(depType string) string {
	switch depType {
	case "postgres":
		return "/var/lib/postgresql/data"
	case "mysql":
		return "/var/lib/mysql"
	case "mongodb":
		return "/data/db"
	case "redis":
		return "/data"
	default:
		return "/data"
	}
}
