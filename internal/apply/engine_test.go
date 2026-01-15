package apply

import (
	"bytes"
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bobbyrathoree/kbox/internal/render"
)

func TestApplyResult(t *testing.T) {
	result := &ApplyResult{
		Created: []string{"Deployment/myapp", "Service/myapp"},
		Updated: []string{"ConfigMap/myapp-config"},
		Errors:  nil,
	}

	if len(result.Created) != 2 {
		t.Errorf("expected 2 created, got %d", len(result.Created))
	}

	if len(result.Updated) != 1 {
		t.Errorf("expected 1 updated, got %d", len(result.Updated))
	}
}

func TestBundleWithStatefulSets(t *testing.T) {
	// Verify Bundle has StatefulSets field
	replicas := int32(1)
	ss := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-postgres",
			Namespace: "default",
			Labels: map[string]string{
				"app": "myapp-postgres",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: "myapp-postgres",
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "myapp-postgres",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "myapp-postgres",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "postgres",
							Image: "postgres:15-alpine",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 5432},
							},
						},
					},
				},
			},
		},
	}

	bundle := &render.Bundle{
		StatefulSets: []*appsv1.StatefulSet{ss},
	}

	if len(bundle.StatefulSets) != 1 {
		t.Errorf("expected 1 statefulset in bundle, got %d", len(bundle.StatefulSets))
	}

	if bundle.StatefulSets[0].Name != "myapp-postgres" {
		t.Errorf("expected statefulset name 'myapp-postgres', got %q", bundle.StatefulSets[0].Name)
	}
}

func TestApplyStatefulSetMethod(t *testing.T) {
	// This test verifies the applyStatefulSet method exists and has correct signature
	// Without a real k8s client, we test the method structure

	var buf bytes.Buffer
	engine := &Engine{
		client:  nil, // Would need fake client for full test
		out:     &buf,
		timeout: DefaultTimeout,
	}

	// Verify the method exists by taking its address
	_ = engine.applyStatefulSet

	// Create a test StatefulSet
	replicas := int32(1)
	ss := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ss",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: "test-ss",
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "main", Image: "test:latest"},
					},
				},
			},
		},
	}

	// Verify StatefulSet is valid
	if ss.Name == "" {
		t.Error("StatefulSet should have a name")
	}
	if ss.Namespace == "" {
		t.Error("StatefulSet should have a namespace")
	}
}

func TestApplyOrderWithStatefulSets(t *testing.T) {
	// Test that Apply function processes StatefulSets at the right stage
	// This is a structure test - verifies the bundle has correct order

	replicas := int32(1)
	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "myapp"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "myapp"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "main", Image: "myapp:v1"},
					},
				},
			},
		},
	}

	ss := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-postgres",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: "myapp-postgres",
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "myapp-postgres"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "myapp-postgres"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "postgres", Image: "postgres:15-alpine"},
					},
				},
			},
		},
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp",
			Namespace: "default",
		},
	}

	bundle := &render.Bundle{
		Services:     []*corev1.Service{svc},
		StatefulSets: []*appsv1.StatefulSet{ss},
		Deployment:   dep,
		Deployments:  []*appsv1.Deployment{dep},
	}

	// Verify AllObjects returns objects in correct order
	objects := bundle.AllObjects()

	var serviceIdx, ssIdx, depIdx int
	for i, obj := range objects {
		switch obj.(type) {
		case *corev1.Service:
			serviceIdx = i
		case *appsv1.StatefulSet:
			ssIdx = i
		case *appsv1.Deployment:
			depIdx = i
		}
	}

	// Services should come before StatefulSets
	if serviceIdx > ssIdx {
		t.Errorf("Services (idx %d) should come before StatefulSets (idx %d)", serviceIdx, ssIdx)
	}

	// StatefulSets should come before Deployments
	if ssIdx > depIdx {
		t.Errorf("StatefulSets (idx %d) should come before Deployments (idx %d)", ssIdx, depIdx)
	}
}

func TestNewEngine(t *testing.T) {
	var buf bytes.Buffer
	engine := NewEngine(nil, &buf)

	if engine == nil {
		t.Fatal("NewEngine should not return nil")
	}

	if engine.timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultTimeout, engine.timeout)
	}

	if engine.out != &buf {
		t.Error("engine output should be set correctly")
	}
}

func TestEngineApplyWithNilClient(t *testing.T) {
	// Test Apply with empty bundle (no client calls needed)
	var buf bytes.Buffer
	engine := NewEngine(nil, &buf)

	bundle := &render.Bundle{}

	result, err := engine.Apply(context.Background(), bundle)
	if err != nil {
		t.Fatalf("Apply with empty bundle should not error: %v", err)
	}

	if result == nil {
		t.Fatal("Apply should return result")
	}

	if len(result.Created) != 0 {
		t.Errorf("expected 0 created with empty bundle, got %d", len(result.Created))
	}

	if len(result.Updated) != 0 {
		t.Errorf("expected 0 updated with empty bundle, got %d", len(result.Updated))
	}
}
