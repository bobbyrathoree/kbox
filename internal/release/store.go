// Package release handles release history storage and rollback
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/bobbyrathoree/kbox/internal/config"
)

const (
	// LabelManagedBy identifies kbox-managed resources
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// LabelApp identifies the application
	LabelApp = "app"
	// LabelReleaseRevision stores the revision number
	LabelReleaseRevision = "kbox.dev/revision"
	// AnnotationReleaseTime stores when the release was created
	AnnotationReleaseTime = "kbox.dev/release-time"
	// AnnotationReleaseImage stores the deployed image
	AnnotationReleaseImage = "kbox.dev/release-image"

	// MaxReleaseHistory is how many releases to keep
	MaxReleaseHistory = 10
)

// Release represents a single deployment release
type Release struct {
	Revision  int       `json:"revision"`
	Timestamp time.Time `json:"timestamp"`
	Image     string    `json:"image"`
	Config    string    `json:"config"` // Serialized AppConfig
}

// Store handles release history persistence using ConfigMaps
type Store struct {
	client    kubernetes.Interface
	namespace string
	appName   string
}

// NewStore creates a new release store
func NewStore(client kubernetes.Interface, namespace, appName string) *Store {
	return &Store{
		client:    client,
		namespace: namespace,
		appName:   appName,
	}
}

// configMapName returns the name of the ConfigMap storing release history
func (s *Store) configMapName() string {
	return fmt.Sprintf("%s-releases", s.appName)
}

// Save stores a new release, returning the revision number
func (s *Store) Save(ctx context.Context, cfg *config.AppConfig) (int, error) {
	// Get existing releases
	releases, err := s.List(ctx)
	if err != nil && !errors.IsNotFound(err) {
		return 0, fmt.Errorf("failed to get existing releases: %w", err)
	}

	// Determine next revision
	nextRevision := 1
	if len(releases) > 0 {
		nextRevision = releases[len(releases)-1].Revision + 1
	}

	// Serialize config
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize config: %w", err)
	}

	// Create new release
	release := Release{
		Revision:  nextRevision,
		Timestamp: time.Now().UTC(),
		Image:     cfg.Spec.Image,
		Config:    string(configJSON),
	}

	// Add to releases
	releases = append(releases, release)

	// Prune old releases (keep last MaxReleaseHistory)
	if len(releases) > MaxReleaseHistory {
		releases = releases[len(releases)-MaxReleaseHistory:]
	}

	// Save to ConfigMap
	if err := s.saveReleases(ctx, releases); err != nil {
		return 0, err
	}

	return nextRevision, nil
}

// List returns all stored releases, sorted by revision
func (s *Store) List(ctx context.Context) ([]Release, error) {
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, s.configMapName(), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get releases ConfigMap: %w", err)
	}

	releasesJSON, ok := cm.Data["releases"]
	if !ok {
		return nil, nil
	}

	var releases []Release
	if err := json.Unmarshal([]byte(releasesJSON), &releases); err != nil {
		return nil, fmt.Errorf("failed to parse releases: %w", err)
	}

	// Sort by revision
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].Revision < releases[j].Revision
	})

	return releases, nil
}

// Get returns a specific release by revision
func (s *Store) Get(ctx context.Context, revision int) (*Release, error) {
	releases, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, r := range releases {
		if r.Revision == revision {
			return &r, nil
		}
	}

	return nil, fmt.Errorf("release %d not found", revision)
}

// GetLatest returns the most recent release
func (s *Store) GetLatest(ctx context.Context) (*Release, error) {
	releases, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}

	return &releases[len(releases)-1], nil
}

// GetPrevious returns the release before the current one
func (s *Store) GetPrevious(ctx context.Context) (*Release, error) {
	releases, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	if len(releases) < 2 {
		return nil, fmt.Errorf("no previous release available (only %d release(s) exist)", len(releases))
	}

	return &releases[len(releases)-2], nil
}

// GetConfig deserializes the config from a release
func (r *Release) GetConfig() (*config.AppConfig, error) {
	var cfg config.AppConfig
	if err := json.Unmarshal([]byte(r.Config), &cfg); err != nil {
		return nil, fmt.Errorf("failed to deserialize config: %w", err)
	}
	return &cfg, nil
}

// saveReleases persists the releases to a ConfigMap
func (s *Store) saveReleases(ctx context.Context, releases []Release) error {
	releasesJSON, err := json.Marshal(releases)
	if err != nil {
		return fmt.Errorf("failed to serialize releases: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.configMapName(),
			Namespace: s.namespace,
			Labels: map[string]string{
				LabelManagedBy: "kbox",
				LabelApp:       s.appName,
			},
			Annotations: map[string]string{
				AnnotationReleaseTime: time.Now().UTC().Format(time.RFC3339),
			},
		},
		Data: map[string]string{
			"releases": string(releasesJSON),
		},
	}

	// Try to get existing ConfigMap
	existing, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, s.configMapName(), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new
			_, err = s.client.CoreV1().ConfigMaps(s.namespace).Create(ctx, cm, metav1.CreateOptions{})
			return err
		}
		return err
	}

	// Update existing
	existing.Data = cm.Data
	existing.Annotations = cm.Annotations
	_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

// Delete removes all release history for an app
func (s *Store) Delete(ctx context.Context) error {
	err := s.client.CoreV1().ConfigMaps(s.namespace).Delete(ctx, s.configMapName(), metav1.DeleteOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// FormatRevision formats a revision number for display
func FormatRevision(revision int) string {
	return "#" + strconv.Itoa(revision)
}
