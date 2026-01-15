package render

import (
	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/secrets"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Bundle contains all rendered Kubernetes objects for an app
type Bundle struct {
	// Objects in apply order
	Namespace    *corev1.Namespace
	ConfigMaps   []*corev1.ConfigMap
	Secrets      []*corev1.Secret
	Services     []*corev1.Service
	StatefulSets []*appsv1.StatefulSet
	Deployments  []*appsv1.Deployment
	Ingresses    []*networkingv1.Ingress
	// Deployment is kept for backward compatibility (points to first deployment)
	Deployment *appsv1.Deployment
}

// AllObjects returns all objects in the bundle in apply order
// Order: Namespace, ConfigMaps, Secrets, Services, StatefulSets, Deployments, Ingresses
func (b *Bundle) AllObjects() []runtime.Object {
	var objects []runtime.Object

	if b.Namespace != nil {
		objects = append(objects, b.Namespace)
	}
	for _, cm := range b.ConfigMaps {
		objects = append(objects, cm)
	}
	for _, s := range b.Secrets {
		objects = append(objects, s)
	}
	for _, svc := range b.Services {
		objects = append(objects, svc)
	}
	// StatefulSets (databases) before Deployments (app)
	for _, ss := range b.StatefulSets {
		objects = append(objects, ss)
	}
	// Use Deployments slice if populated, otherwise fall back to single Deployment
	if len(b.Deployments) > 0 {
		for _, dep := range b.Deployments {
			objects = append(objects, dep)
		}
	} else if b.Deployment != nil {
		objects = append(objects, b.Deployment)
	}
	// Ingresses last (depends on Services)
	for _, ing := range b.Ingresses {
		objects = append(objects, ing)
	}

	return objects
}

// Renderer renders kbox config into Kubernetes objects
type Renderer struct {
	config *config.AppConfig
}

// New creates a new renderer for the given config
func New(cfg *config.AppConfig) *Renderer {
	return &Renderer{config: cfg}
}

// Render renders all Kubernetes objects from the config
func (r *Renderer) Render() (*Bundle, error) {
	bundle := &Bundle{}

	// Render dependencies first (databases, caches)
	var depEnvVars map[string]string
	if len(r.config.Spec.Dependencies) > 0 {
		statefulSets, depServices, depSecrets, envVars, err := r.RenderAllDependencies()
		if err != nil {
			return nil, err
		}
		bundle.StatefulSets = statefulSets
		bundle.Services = append(bundle.Services, depServices...)
		bundle.Secrets = append(bundle.Secrets, depSecrets...)
		depEnvVars = envVars
	}

	// Render Deployment (with injected dependency env vars)
	deployment, err := r.RenderDeployment()
	if err != nil {
		return nil, err
	}

	// Inject dependency environment variables into the app deployment
	if len(depEnvVars) > 0 {
		for i := range deployment.Spec.Template.Spec.Containers {
			for k, v := range depEnvVars {
				deployment.Spec.Template.Spec.Containers[i].Env = append(
					deployment.Spec.Template.Spec.Containers[i].Env,
					corev1.EnvVar{Name: k, Value: v},
				)
			}
		}
	}

	bundle.Deployment = deployment
	bundle.Deployments = []*appsv1.Deployment{deployment}

	// Render Service
	service, err := r.RenderService()
	if err != nil {
		return nil, err
	}
	bundle.Services = append(bundle.Services, service)

	// Render ConfigMap for env vars if any
	if len(r.config.Spec.Env) > 0 {
		cm, err := r.RenderConfigMap()
		if err != nil {
			return nil, err
		}
		bundle.ConfigMaps = append(bundle.ConfigMaps, cm)
	}

	// Render Secrets from .env file if configured
	if r.config.Spec.Secrets != nil && r.config.Spec.Secrets.FromEnvFile != "" {
		secret, err := r.RenderSecretFromEnvFile()
		if err != nil {
			return nil, err
		}
		bundle.Secrets = append(bundle.Secrets, secret)
	}

	// Render Secrets from SOPS-encrypted files if configured
	if r.config.Spec.Secrets != nil && len(r.config.Spec.Secrets.FromSops) > 0 {
		secret, err := r.RenderSecretFromSops()
		if err != nil {
			return nil, err
		}
		bundle.Secrets = append(bundle.Secrets, secret)
	}

	// Render Ingress if configured
	if r.config.Spec.Ingress != nil && r.config.Spec.Ingress.Enabled {
		ingress, err := r.RenderIngress()
		if err != nil {
			return nil, err
		}
		bundle.Ingresses = append(bundle.Ingresses, ingress)
	}

	return bundle, nil
}

// RenderSecretFromEnvFile creates a Secret from a .env file
func (r *Renderer) RenderSecretFromEnvFile() (*corev1.Secret, error) {
	envFile := r.config.Spec.Secrets.FromEnvFile
	secretName := r.config.Metadata.Name + "-secrets"

	secret, err := secrets.LoadAndCreateSecret(envFile, secretName, r.Namespace(), r.Labels())
	if err != nil {
		return nil, err
	}

	return secret, nil
}

// RenderSecretFromSops creates a Secret from SOPS-encrypted files
func (r *Renderer) RenderSecretFromSops() (*corev1.Secret, error) {
	sopsFiles := r.config.Spec.Secrets.FromSops
	secretName := r.config.Metadata.Name + "-sops-secrets"

	secret, err := secrets.LoadSopsAndCreateSecret(sopsFiles, secretName, r.Namespace(), r.Labels())
	if err != nil {
		return nil, err
	}

	return secret, nil
}

// Labels returns standard labels for the app
func (r *Renderer) Labels() map[string]string {
	return map[string]string{
		"app":                          r.config.Metadata.Name,
		"app.kubernetes.io/name":       r.config.Metadata.Name,
		"app.kubernetes.io/managed-by": "kbox",
	}
}

// Selector returns the pod selector for the app
func (r *Renderer) Selector() map[string]string {
	return map[string]string{
		"app": r.config.Metadata.Name,
	}
}

// Namespace returns the target namespace
func (r *Renderer) Namespace() string {
	if r.config.Metadata.Namespace != "" {
		return r.config.Metadata.Namespace
	}
	return "default"
}
