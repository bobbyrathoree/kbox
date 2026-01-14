package release

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/bobbyrathoree/kbox/internal/config"
)

func TestReleaseSaveAndRetrieve(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec: config.AppSpec{
			Image:    "myapp:v1.0.0",
			Port:     8080,
			Replicas: 3,
		},
	}

	// Save a release
	rev, err := store.Save(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to save release: %v", err)
	}
	if rev != 1 {
		t.Errorf("expected revision 1, got %d", rev)
	}

	// Retrieve the release
	releases, err := store.List(ctx)
	if err != nil {
		t.Fatalf("failed to list releases: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("expected 1 release, got %d", len(releases))
	}

	if releases[0].Revision != 1 {
		t.Errorf("expected revision 1, got %d", releases[0].Revision)
	}
	if releases[0].Image != "myapp:v1.0.0" {
		t.Errorf("expected image myapp:v1.0.0, got %s", releases[0].Image)
	}
}

func TestReleaseIncrementsRevision(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v1"},
	}

	// Save multiple releases
	for i := 1; i <= 3; i++ {
		cfg.Spec.Image = "myapp:v" + string(rune('0'+i))
		rev, err := store.Save(ctx, cfg)
		if err != nil {
			t.Fatalf("save %d failed: %v", i, err)
		}
		if rev != i {
			t.Errorf("expected revision %d, got %d", i, rev)
		}
	}

	releases, _ := store.List(ctx)
	if len(releases) != 3 {
		t.Errorf("expected 3 releases, got %d", len(releases))
	}
}

func TestReleaseHistoryPruning(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v1"},
	}

	// Save more than MaxReleaseHistory releases
	for i := 1; i <= MaxReleaseHistory+5; i++ {
		cfg.Spec.Image = "myapp:v" + string(rune('0'+i))
		store.Save(ctx, cfg)
	}

	releases, _ := store.List(ctx)
	if len(releases) != MaxReleaseHistory {
		t.Errorf("expected %d releases (pruned), got %d", MaxReleaseHistory, len(releases))
	}

	// First release should be 6 (oldest 5 were pruned)
	if releases[0].Revision != 6 {
		t.Errorf("expected first revision to be 6 (after pruning), got %d", releases[0].Revision)
	}
}

func TestReleaseGetByRevision(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v1"},
	}

	// Save releases
	store.Save(ctx, cfg)
	cfg.Spec.Image = "myapp:v2"
	store.Save(ctx, cfg)
	cfg.Spec.Image = "myapp:v3"
	store.Save(ctx, cfg)

	// Get specific revision
	rel, err := store.Get(ctx, 2)
	if err != nil {
		t.Fatalf("failed to get revision 2: %v", err)
	}
	if rel.Revision != 2 {
		t.Errorf("expected revision 2, got %d", rel.Revision)
	}
	if rel.Image != "myapp:v2" {
		t.Errorf("expected image myapp:v2, got %s", rel.Image)
	}

	// Get non-existent revision
	_, err = store.Get(ctx, 99)
	if err == nil {
		t.Error("expected error for non-existent revision")
	}
}

func TestReleaseGetLatest(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v1"},
	}

	// Save releases
	store.Save(ctx, cfg)
	cfg.Spec.Image = "myapp:v2"
	store.Save(ctx, cfg)

	latest, err := store.GetLatest(ctx)
	if err != nil {
		t.Fatalf("failed to get latest: %v", err)
	}
	if latest.Revision != 2 {
		t.Errorf("expected latest revision 2, got %d", latest.Revision)
	}
}

func TestReleaseGetPrevious(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v1"},
	}

	// Only one release - GetPrevious should fail
	store.Save(ctx, cfg)
	_, err := store.GetPrevious(ctx)
	if err == nil {
		t.Error("expected error when only one release exists")
	}

	// Add second release - now GetPrevious should work
	cfg.Spec.Image = "myapp:v2"
	store.Save(ctx, cfg)

	prev, err := store.GetPrevious(ctx)
	if err != nil {
		t.Fatalf("failed to get previous: %v", err)
	}
	if prev.Revision != 1 {
		t.Errorf("expected previous revision 1, got %d", prev.Revision)
	}
	if prev.Image != "myapp:v1" {
		t.Errorf("expected image myapp:v1, got %s", prev.Image)
	}
}

func TestReleaseConfigDeserialization(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp", Namespace: "prod"},
		Spec: config.AppSpec{
			Image:       "myapp:v1.0.0",
			Port:        3000,
			Replicas:    5,
			HealthCheck: "/health",
			Env: map[string]string{
				"LOG_LEVEL": "warn",
			},
		},
	}

	store.Save(ctx, cfg)

	// Retrieve and deserialize
	rel, _ := store.GetLatest(ctx)
	restored, err := rel.GetConfig()
	if err != nil {
		t.Fatalf("failed to deserialize config: %v", err)
	}

	// Verify all fields restored
	if restored.Metadata.Name != "myapp" {
		t.Errorf("expected name myapp, got %s", restored.Metadata.Name)
	}
	if restored.Metadata.Namespace != "prod" {
		t.Errorf("expected namespace prod, got %s", restored.Metadata.Namespace)
	}
	if restored.Spec.Port != 3000 {
		t.Errorf("expected port 3000, got %d", restored.Spec.Port)
	}
	if restored.Spec.Replicas != 5 {
		t.Errorf("expected replicas 5, got %d", restored.Spec.Replicas)
	}
	if restored.Spec.Env["LOG_LEVEL"] != "warn" {
		t.Errorf("expected LOG_LEVEL=warn, got %s", restored.Spec.Env["LOG_LEVEL"])
	}
}

func TestReleaseNamespaceIsolation(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()

	// Create stores for different namespaces
	store1 := NewStore(client, "ns1", "myapp")
	store2 := NewStore(client, "ns2", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v1"},
	}

	// Save in ns1
	store1.Save(ctx, cfg)

	// Save in ns2
	cfg.Spec.Image = "myapp:v2"
	store2.Save(ctx, cfg)

	// Each should only see their own releases
	releases1, _ := store1.List(ctx)
	releases2, _ := store2.List(ctx)

	if len(releases1) != 1 || len(releases2) != 1 {
		t.Error("releases should be isolated by namespace")
	}
	if releases1[0].Image != "myapp:v1" {
		t.Errorf("ns1 should have v1, got %s", releases1[0].Image)
	}
	if releases2[0].Image != "myapp:v2" {
		t.Errorf("ns2 should have v2, got %s", releases2[0].Image)
	}
}

func TestEmptyReleaseList(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	releases, err := store.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(releases) != 0 {
		t.Errorf("expected empty list, got %d", len(releases))
	}

	_, err = store.GetLatest(ctx)
	if err == nil {
		t.Error("expected error when no releases exist")
	}
}

func TestFormatRevision(t *testing.T) {
	tests := []struct {
		revision int
		expected string
	}{
		{1, "#1"},
		{10, "#10"},
		{100, "#100"},
	}

	for _, tt := range tests {
		got := FormatRevision(tt.revision)
		if got != tt.expected {
			t.Errorf("FormatRevision(%d) = %s, want %s", tt.revision, got, tt.expected)
		}
	}
}

func TestReleaseTimestamp(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	before := time.Now().UTC()

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v1"},
	}
	store.Save(ctx, cfg)

	after := time.Now().UTC()

	rel, _ := store.GetLatest(ctx)

	if rel.Timestamp.Before(before) || rel.Timestamp.After(after) {
		t.Errorf("timestamp %v should be between %v and %v", rel.Timestamp, before, after)
	}
}

func TestExistingConfigMapUpdate(t *testing.T) {
	ctx := context.Background()

	// Pre-create a ConfigMap
	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-releases",
			Namespace: "default",
		},
		Data: map[string]string{
			"releases": `[{"revision":1,"timestamp":"2024-01-01T00:00:00Z","image":"old:v1","config":"{}"}]`,
		},
	}

	client := fake.NewSimpleClientset(existing)
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v2"},
	}

	// Should update the existing ConfigMap and increment revision
	rev, err := store.Save(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to save: %v", err)
	}
	if rev != 2 {
		t.Errorf("expected revision 2 (incrementing from existing), got %d", rev)
	}

	releases, _ := store.List(ctx)
	if len(releases) != 2 {
		t.Errorf("expected 2 releases, got %d", len(releases))
	}
}

func TestConfigMapLabels(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	store := NewStore(client, "default", "myapp")

	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp"},
		Spec:     config.AppSpec{Image: "myapp:v1"},
	}
	store.Save(ctx, cfg)

	// Check that ConfigMap has correct labels
	cm, err := client.CoreV1().ConfigMaps("default").Get(ctx, "myapp-releases", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get configmap: %v", err)
	}

	if cm.Labels[LabelManagedBy] != "kbox" {
		t.Errorf("expected managed-by label 'kbox', got %s", cm.Labels[LabelManagedBy])
	}
	if cm.Labels[LabelApp] != "myapp" {
		t.Errorf("expected app label 'myapp', got %s", cm.Labels[LabelApp])
	}
}

// TestRollbackRestoresConfig verifies the full rollback cycle
func TestRollbackRestoresConfig(t *testing.T) {
	// Create a release with full config
	release := Release{
		Revision:  1,
		Timestamp: time.Now().UTC(),
		Image:     "myapp:v1.0.0",
	}

	// Serialize config into release
	cfg := &config.AppConfig{
		Metadata: config.Metadata{Name: "myapp", Namespace: "prod"},
		Spec: config.AppSpec{
			Image:    "myapp:v1.0.0",
			Port:     8080,
			Replicas: 3,
			Env: map[string]string{
				"KEY": "value",
			},
		},
	}
	configJSON, _ := json.Marshal(cfg)
	release.Config = string(configJSON)

	// Verify we can restore it
	restored, err := release.GetConfig()
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}

	if restored.Spec.Image != "myapp:v1.0.0" {
		t.Errorf("expected image myapp:v1.0.0, got %s", restored.Spec.Image)
	}
	if restored.Spec.Replicas != 3 {
		t.Errorf("expected replicas 3, got %d", restored.Spec.Replicas)
	}
}
