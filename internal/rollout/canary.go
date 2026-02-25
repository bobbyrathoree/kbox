package rollout

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// CanarySuffix is appended to deployment name for canary
	CanarySuffix = "-canary"

	// CanaryLabel marks a deployment as canary
	CanaryLabel = "kbox.dev/canary"
)

// CanaryConfig holds canary deployment configuration
type CanaryConfig struct {
	// Weight is the percentage of traffic for canary (1-100)
	Weight int

	// Image to deploy for canary (if different from current)
	Image string
}

// CanaryStatus represents the current canary state
type CanaryStatus struct {
	// Active indicates if a canary deployment exists
	Active bool

	// CanaryName is the canary deployment name
	CanaryName string

	// MainReplicas is the number of main deployment replicas
	MainReplicas int32

	// CanaryReplicas is the number of canary replicas
	CanaryReplicas int32

	// CanaryImage is the image used by canary
	CanaryImage string

	// MainImage is the image used by main deployment
	MainImage string

	// Weight is the approximate traffic percentage to canary
	Weight int
}

// StartCanary creates a canary deployment with the specified weight
func StartCanary(ctx context.Context, client *kubernetes.Clientset, namespace, name string, cfg CanaryConfig) (*CanaryStatus, error) {
	canaryName := name + CanarySuffix

	// Check if canary already exists
	_, err := client.AppsV1().Deployments(namespace).Get(ctx, canaryName, metav1.GetOptions{})
	if err == nil {
		return nil, fmt.Errorf("canary deployment %q already exists\n  Promote or abort first:\n    kbox rollout promote\n    kbox rollout undo", canaryName)
	}
	if !errors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check for existing canary: %w", err)
	}

	// Get the main deployment
	mainDeploy, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("deployment %q not found: %w", name, err)
	}

	// Calculate replicas based on weight
	// If main has 10 replicas and weight is 20%, canary gets 2 replicas
	totalReplicas := int32(1)
	if mainDeploy.Spec.Replicas != nil {
		totalReplicas = *mainDeploy.Spec.Replicas
	}

	// Calculate canary replicas (minimum 1)
	canaryReplicas := int32(float64(totalReplicas) * float64(cfg.Weight) / 100.0)
	if canaryReplicas < 1 {
		canaryReplicas = 1
	}

	// Determine the image for canary
	canaryImage := cfg.Image
	if canaryImage == "" && len(mainDeploy.Spec.Template.Spec.Containers) > 0 {
		canaryImage = mainDeploy.Spec.Template.Spec.Containers[0].Image
	}

	// Create canary deployment by copying the main one
	canaryDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      canaryName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       name, // Same app label so Service routes to both
				CanaryLabel: "true",
			},
			Annotations: map[string]string{
				"kbox.dev/canary-for": name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &canaryReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":       name,
					CanaryLabel: "true",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":       name,
						CanaryLabel: "true",
					},
				},
				Spec: *mainDeploy.Spec.Template.Spec.DeepCopy(),
			},
		},
	}

	// Update the image in canary
	if len(canaryDeploy.Spec.Template.Spec.Containers) > 0 {
		canaryDeploy.Spec.Template.Spec.Containers[0].Image = canaryImage
	}

	// Create the canary deployment
	_, err = client.AppsV1().Deployments(namespace).Create(ctx, canaryDeploy, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create canary deployment: %w", err)
	}

	// Calculate actual weight
	actualWeight := int(float64(canaryReplicas) / float64(totalReplicas+canaryReplicas) * 100)

	mainImage := ""
	if len(mainDeploy.Spec.Template.Spec.Containers) > 0 {
		mainImage = mainDeploy.Spec.Template.Spec.Containers[0].Image
	}

	return &CanaryStatus{
		Active:         true,
		CanaryName:     canaryName,
		MainReplicas:   totalReplicas,
		CanaryReplicas: canaryReplicas,
		CanaryImage:    canaryImage,
		MainImage:      mainImage,
		Weight:         actualWeight,
	}, nil
}

// GetCanaryStatus returns the current canary status
func GetCanaryStatus(ctx context.Context, client *kubernetes.Clientset, namespace, name string) (*CanaryStatus, error) {
	canaryName := name + CanarySuffix

	// Try to get canary deployment
	canaryDeploy, err := client.AppsV1().Deployments(namespace).Get(ctx, canaryName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return &CanaryStatus{Active: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get canary deployment: %w", err)
	}

	// Get main deployment
	mainDeploy, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get main deployment: %w", err)
	}

	mainReplicas := int32(1)
	if mainDeploy.Spec.Replicas != nil {
		mainReplicas = *mainDeploy.Spec.Replicas
	}
	canaryReplicas := int32(1)
	if canaryDeploy.Spec.Replicas != nil {
		canaryReplicas = *canaryDeploy.Spec.Replicas
	}
	totalReplicas := mainReplicas + canaryReplicas

	weight := 0
	if totalReplicas > 0 {
		weight = int(float64(canaryReplicas) / float64(totalReplicas) * 100)
	}

	mainImage := ""
	if len(mainDeploy.Spec.Template.Spec.Containers) > 0 {
		mainImage = mainDeploy.Spec.Template.Spec.Containers[0].Image
	}

	canaryImage := ""
	if len(canaryDeploy.Spec.Template.Spec.Containers) > 0 {
		canaryImage = canaryDeploy.Spec.Template.Spec.Containers[0].Image
	}

	return &CanaryStatus{
		Active:         true,
		CanaryName:     canaryName,
		MainReplicas:   mainReplicas,
		CanaryReplicas: canaryReplicas,
		CanaryImage:    canaryImage,
		MainImage:      mainImage,
		Weight:         weight,
	}, nil
}

// PromoteCanary promotes the canary to become the main deployment
func PromoteCanary(ctx context.Context, client *kubernetes.Clientset, namespace, name string) error {
	canaryName := name + CanarySuffix

	// Get canary deployment
	canaryDeploy, err := client.AppsV1().Deployments(namespace).Get(ctx, canaryName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return fmt.Errorf("no canary deployment found for %q", name)
	}
	if err != nil {
		return fmt.Errorf("failed to get canary deployment: %w", err)
	}

	// Get main deployment
	mainDeploy, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get main deployment: %w", err)
	}

	// Update main deployment with canary's image
	if len(canaryDeploy.Spec.Template.Spec.Containers) > 0 && len(mainDeploy.Spec.Template.Spec.Containers) > 0 {
		mainDeploy.Spec.Template.Spec.Containers[0].Image = canaryDeploy.Spec.Template.Spec.Containers[0].Image
	}

	// Update main deployment
	_, err = client.AppsV1().Deployments(namespace).Update(ctx, mainDeploy, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update main deployment: %w", err)
	}

	// Delete canary deployment
	err = client.AppsV1().Deployments(namespace).Delete(ctx, canaryName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete canary deployment: %w", err)
	}

	return nil
}

// AbortCanary deletes the canary deployment without promoting
func AbortCanary(ctx context.Context, client *kubernetes.Clientset, namespace, name string) error {
	canaryName := name + CanarySuffix

	// Check if canary exists
	_, err := client.AppsV1().Deployments(namespace).Get(ctx, canaryName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return fmt.Errorf("no canary deployment found for %q", name)
	}
	if err != nil {
		return fmt.Errorf("failed to get canary deployment: %w", err)
	}

	// Delete canary deployment
	err = client.AppsV1().Deployments(namespace).Delete(ctx, canaryName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete canary deployment: %w", err)
	}

	return nil
}
