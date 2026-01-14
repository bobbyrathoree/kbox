package render

import (
	"github.com/bobbyrathoree/kbox/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Bundle contains all rendered Kubernetes objects for an app
type Bundle struct {
	// Objects in apply order
	Namespace  *corev1.Namespace
	ConfigMaps []*corev1.ConfigMap
	Secrets    []*corev1.Secret
	Services   []*corev1.Service
	Deployment *appsv1.Deployment
}

// AllObjects returns all objects in the bundle in apply order
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
	if b.Deployment != nil {
		objects = append(objects, b.Deployment)
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

	// Render Deployment
	deployment, err := r.RenderDeployment()
	if err != nil {
		return nil, err
	}
	bundle.Deployment = deployment

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

	return bundle, nil
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
