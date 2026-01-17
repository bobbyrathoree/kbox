package database

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/bobbyrathoree/kbox/internal/dependencies"
)

// DatabaseInfo contains information about a discovered database
type DatabaseInfo struct {
	// Type is the database type (postgres, redis, etc.)
	Type string

	// ServiceName is the K8s service name
	ServiceName string

	// PodName is the StatefulSet pod name (service-0)
	PodName string

	// Namespace where the database is deployed
	Namespace string

	// AppName is the owning app name (from kbox.dev/app label)
	AppName string

	// Ready indicates if the pod is ready
	Ready bool
}

// FindByType finds databases of a specific type in the namespace
func FindByType(ctx context.Context, client *kubernetes.Clientset, namespace, dbType string) ([]DatabaseInfo, error) {
	// Validate database type
	if !dependencies.IsSupported(dbType) {
		return nil, fmt.Errorf("unsupported database type %q\n  Supported: %s", dbType, strings.Join(dependencies.SupportedTypes(), ", "))
	}

	// List StatefulSets with the dependency label
	labelSelector := fmt.Sprintf("kbox.dev/dependency=%s", dbType)
	ssList, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list StatefulSets: %w", err)
	}

	if len(ssList.Items) == 0 {
		return nil, fmt.Errorf("no %s database found\n  Add one with: kbox add %s", dbType, dbType)
	}

	var databases []DatabaseInfo
	for _, ss := range ssList.Items {
		// Get the pod for this StatefulSet
		podName := fmt.Sprintf("%s-0", ss.Name)

		// Check pod readiness
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		ready := false
		if err == nil {
			for _, cond := range pod.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					ready = true
					break
				}
			}
		}

		databases = append(databases, DatabaseInfo{
			Type:        dbType,
			ServiceName: ss.Name,
			PodName:     podName,
			Namespace:   namespace,
			AppName:     ss.Labels["kbox.dev/app"],
			Ready:       ready,
		})
	}

	return databases, nil
}

// FindByServiceName finds a database by its service name
func FindByServiceName(ctx context.Context, client *kubernetes.Clientset, namespace, serviceName string) (*DatabaseInfo, error) {
	// Get the StatefulSet
	ss, err := client.AppsV1().StatefulSets(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("database %q not found in namespace %q", serviceName, namespace)
	}

	// Check if it's a kbox-managed dependency
	dbType, ok := ss.Labels["kbox.dev/dependency"]
	if !ok {
		return nil, fmt.Errorf("%q is not a kbox-managed database", serviceName)
	}

	// Get the pod
	podName := fmt.Sprintf("%s-0", ss.Name)
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	ready := false
	if err == nil {
		for _, cond := range pod.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready = true
				break
			}
		}
	}

	return &DatabaseInfo{
		Type:        dbType,
		ServiceName: ss.Name,
		PodName:     podName,
		Namespace:   namespace,
		AppName:     ss.Labels["kbox.dev/app"],
		Ready:       ready,
	}, nil
}

// ResolveDatabase finds a database by type or service name
// If dbSpec is a known type (postgres, redis, etc.), it finds by type
// Otherwise it treats it as a service name
func ResolveDatabase(ctx context.Context, client *kubernetes.Clientset, namespace, dbSpec string) (*DatabaseInfo, error) {
	// Check if it's a database type
	if dependencies.IsSupported(dbSpec) {
		databases, err := FindByType(ctx, client, namespace, dbSpec)
		if err != nil {
			return nil, err
		}

		if len(databases) == 1 {
			return &databases[0], nil
		}

		// Multiple databases found - need user to specify
		var names []string
		for _, db := range databases {
			names = append(names, db.ServiceName)
		}
		return nil, fmt.Errorf("multiple %s databases found: %s\n  Specify with: kbox db connect <service-name>", dbSpec, strings.Join(names, ", "))
	}

	// Treat as service name
	return FindByServiceName(ctx, client, namespace, dbSpec)
}
