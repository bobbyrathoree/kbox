package preview

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// Label keys
	LabelPreview     = "kbox.dev/preview"
	LabelApp         = "kbox.dev/app"
	LabelPreviewName = "kbox.dev/preview-name"
)

// PreviewInfo contains information about a preview environment
type PreviewInfo struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	App       string    `json:"app"`
	Created   time.Time `json:"created"`
	Status    string    `json:"status"`
}

// Manager handles preview environment lifecycle
type Manager struct {
	client  kubernetes.Interface
	appName string
}

// NewManager creates a new preview manager
func NewManager(client kubernetes.Interface, appName string) *Manager {
	return &Manager{
		client:  client,
		appName: appName,
	}
}

// Create creates a new preview environment namespace
func (m *Manager) Create(ctx context.Context, name string) (*PreviewInfo, error) {
	nsName := m.namespaceName(name)

	// Check if namespace already exists
	_, err := m.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if err == nil {
		return nil, fmt.Errorf("preview %q already exists\n  → Run 'kbox preview destroy --name=%s' first", name, name)
	}
	if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check namespace: %w", err)
	}

	// Create namespace with labels
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				LabelPreview:     "true",
				LabelApp:         m.appName,
				LabelPreviewName: name,
			},
			Annotations: map[string]string{
				"kbox.dev/created-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	created, err := m.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create preview namespace: %w", err)
	}

	return &PreviewInfo{
		Name:      name,
		Namespace: created.Name,
		App:       m.appName,
		Created:   created.CreationTimestamp.Time,
		Status:    string(created.Status.Phase),
	}, nil
}

// Destroy deletes a preview environment and all its resources
func (m *Manager) Destroy(ctx context.Context, name string) error {
	nsName := m.namespaceName(name)

	// Check if namespace exists
	ns, err := m.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("preview %q not found\n  → Run 'kbox preview list' to see active previews", name)
	}
	if err != nil {
		return fmt.Errorf("failed to get namespace: %w", err)
	}

	// Verify it's a preview namespace for this app
	if ns.Labels[LabelPreview] != "true" || ns.Labels[LabelApp] != m.appName {
		return fmt.Errorf("namespace %q is not a preview for app %q", nsName, m.appName)
	}

	// Delete the namespace (cascades to all resources)
	err = m.client.CoreV1().Namespaces().Delete(ctx, nsName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete preview namespace: %w", err)
	}

	return nil
}

// List returns all preview environments for the app
func (m *Manager) List(ctx context.Context) ([]PreviewInfo, error) {
	// List namespaces with preview labels for this app
	selector := fmt.Sprintf("%s=true,%s=%s", LabelPreview, LabelApp, m.appName)

	nsList, err := m.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list preview namespaces: %w", err)
	}

	var previews []PreviewInfo
	for _, ns := range nsList.Items {
		previews = append(previews, PreviewInfo{
			Name:      ns.Labels[LabelPreviewName],
			Namespace: ns.Name,
			App:       m.appName,
			Created:   ns.CreationTimestamp.Time,
			Status:    string(ns.Status.Phase),
		})
	}

	return previews, nil
}

// Get returns information about a specific preview
func (m *Manager) Get(ctx context.Context, name string) (*PreviewInfo, error) {
	nsName := m.namespaceName(name)

	ns, err := m.client.CoreV1().Namespaces().Get(ctx, nsName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("preview %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get preview: %w", err)
	}

	// Verify it's a preview namespace
	if ns.Labels[LabelPreview] != "true" || ns.Labels[LabelApp] != m.appName {
		return nil, fmt.Errorf("namespace %q is not a preview for app %q", nsName, m.appName)
	}

	return &PreviewInfo{
		Name:      name,
		Namespace: ns.Name,
		App:       m.appName,
		Created:   ns.CreationTimestamp.Time,
		Status:    string(ns.Status.Phase),
	}, nil
}

// namespaceName generates the namespace name for a preview
func (m *Manager) namespaceName(previewName string) string {
	return fmt.Sprintf("%s-preview-%s", m.appName, previewName)
}

// NamespaceName returns the namespace name for a preview (exported for use by CLI)
func (m *Manager) NamespaceName(previewName string) string {
	return m.namespaceName(previewName)
}
