package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadYAMLWithComments(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-yaml-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	content := `# Top-level comment
apiVersion: kbox.dev/v1
kind: App
metadata:
  name: testapp
spec:
  image: testapp:v1
  # Port comment
  port: 8080
`
	configPath := filepath.Join(tmpDir, "kbox.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	node, err := LoadYAMLWithComments(configPath)
	if err != nil {
		t.Fatalf("failed to load YAML: %v", err)
	}

	if node == nil {
		t.Fatal("expected non-nil node")
	}
	if node.Kind != yaml.DocumentNode {
		t.Errorf("expected DocumentNode, got %v", node.Kind)
	}
}

func TestSaveYAMLWithComments(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-yaml-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a YAML with comments
	content := `# My awesome app config
apiVersion: kbox.dev/v1
kind: App
metadata:
  name: testapp
spec:
  # Docker image for the app
  image: testapp:v1
  port: 8080
`
	configPath := filepath.Join(tmpDir, "kbox.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Load and save
	node, err := LoadYAMLWithComments(configPath)
	if err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(tmpDir, "output.yaml")
	if err := SaveYAMLWithComments(outputPath, node); err != nil {
		t.Fatalf("failed to save YAML: %v", err)
	}

	// Read back and verify comments are preserved
	saved, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}

	savedStr := string(saved)
	if !strings.Contains(savedStr, "# My awesome app config") {
		t.Error("top-level comment not preserved")
	}
	if !strings.Contains(savedStr, "# Docker image for the app") {
		t.Error("inline comment not preserved")
	}
}

func TestFindMapKey(t *testing.T) {
	content := `apiVersion: kbox.dev/v1
kind: App
metadata:
  name: testapp
spec:
  image: testapp:v1
`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		t.Fatal(err)
	}

	root := GetRootDocument(&node)

	// Test finding existing keys
	apiVersion := FindMapKey(root, "apiVersion")
	if apiVersion == nil {
		t.Fatal("expected to find apiVersion")
	}
	if apiVersion.Value != "kbox.dev/v1" {
		t.Errorf("expected 'kbox.dev/v1', got %q", apiVersion.Value)
	}

	metadata := FindMapKey(root, "metadata")
	if metadata == nil {
		t.Fatal("expected to find metadata")
	}

	name := FindMapKey(metadata, "name")
	if name == nil {
		t.Fatal("expected to find name")
	}
	if name.Value != "testapp" {
		t.Errorf("expected 'testapp', got %q", name.Value)
	}

	// Test non-existent key
	notFound := FindMapKey(root, "nonexistent")
	if notFound != nil {
		t.Error("expected nil for non-existent key")
	}

	// Test nil node
	if FindMapKey(nil, "test") != nil {
		t.Error("expected nil for nil node")
	}
}

func TestAddToSequence(t *testing.T) {
	seq := &yaml.Node{Kind: yaml.SequenceNode}

	item1 := &yaml.Node{Kind: yaml.ScalarNode, Value: "item1"}
	item2 := &yaml.Node{Kind: yaml.ScalarNode, Value: "item2"}

	AddToSequence(seq, item1)
	AddToSequence(seq, item2)

	if len(seq.Content) != 2 {
		t.Errorf("expected 2 items, got %d", len(seq.Content))
	}
	if seq.Content[0].Value != "item1" {
		t.Errorf("expected 'item1', got %q", seq.Content[0].Value)
	}
	if seq.Content[1].Value != "item2" {
		t.Errorf("expected 'item2', got %q", seq.Content[1].Value)
	}

	// Test nil sequence
	AddToSequence(nil, item1) // Should not panic
}

func TestRemoveFromSequence(t *testing.T) {
	content := `- type: postgres
  version: "15"
- type: redis
  version: "7"
- type: mongodb
`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		t.Fatal(err)
	}

	seq := GetRootDocument(&node)

	// Remove redis
	removed := RemoveFromSequence(seq, "type", "redis")
	if !removed {
		t.Error("expected redis to be removed")
	}
	if len(seq.Content) != 2 {
		t.Errorf("expected 2 items after removal, got %d", len(seq.Content))
	}

	// Verify remaining items
	if !SequenceContains(seq, "type", "postgres") {
		t.Error("postgres should still exist")
	}
	if !SequenceContains(seq, "type", "mongodb") {
		t.Error("mongodb should still exist")
	}
	if SequenceContains(seq, "type", "redis") {
		t.Error("redis should not exist after removal")
	}

	// Try to remove non-existent item
	removed = RemoveFromSequence(seq, "type", "mysql")
	if removed {
		t.Error("expected false for non-existent item")
	}

	// Test nil sequence
	if RemoveFromSequence(nil, "type", "test") {
		t.Error("expected false for nil sequence")
	}
}

func TestSequenceContains(t *testing.T) {
	content := `- type: postgres
- type: redis
`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		t.Fatal(err)
	}

	seq := GetRootDocument(&node)

	if !SequenceContains(seq, "type", "postgres") {
		t.Error("expected to find postgres")
	}
	if !SequenceContains(seq, "type", "redis") {
		t.Error("expected to find redis")
	}
	if SequenceContains(seq, "type", "mysql") {
		t.Error("expected not to find mysql")
	}

	// Test nil sequence
	if SequenceContains(nil, "type", "test") {
		t.Error("expected false for nil sequence")
	}
}

func TestDependencyToNode(t *testing.T) {
	dep := &DependencyConfig{
		Type:    "postgres",
		Version: "15",
		Storage: "10Gi",
	}

	node := DependencyToNode(dep)

	if node.Kind != yaml.MappingNode {
		t.Errorf("expected MappingNode, got %v", node.Kind)
	}

	// Should have 6 content items (3 key-value pairs)
	if len(node.Content) != 6 {
		t.Errorf("expected 6 content items, got %d", len(node.Content))
	}

	// Verify type
	if node.Content[0].Value != "type" || node.Content[1].Value != "postgres" {
		t.Error("type field not correct")
	}

	// Verify version
	if node.Content[2].Value != "version" || node.Content[3].Value != "15" {
		t.Error("version field not correct")
	}

	// Verify storage
	if node.Content[4].Value != "storage" || node.Content[5].Value != "10Gi" {
		t.Error("storage field not correct")
	}
}

func TestDependencyToNode_MinimalFields(t *testing.T) {
	dep := &DependencyConfig{
		Type: "redis",
	}

	node := DependencyToNode(dep)

	// Should only have 2 content items (1 key-value pair for type)
	if len(node.Content) != 2 {
		t.Errorf("expected 2 content items, got %d", len(node.Content))
	}

	if node.Content[0].Value != "type" || node.Content[1].Value != "redis" {
		t.Error("type field not correct")
	}
}

func TestGetRootDocument(t *testing.T) {
	content := `name: test`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		t.Fatal(err)
	}

	root := GetRootDocument(&node)
	if root == nil {
		t.Fatal("expected non-nil root")
	}
	if root.Kind != yaml.MappingNode {
		t.Errorf("expected MappingNode, got %v", root.Kind)
	}

	// Test nil node
	if GetRootDocument(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestEnsureDependenciesNode(t *testing.T) {
	content := `apiVersion: kbox.dev/v1
spec:
  image: test:v1
`
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		t.Fatal(err)
	}

	root := GetRootDocument(&node)
	depsNode := EnsureDependenciesNode(root)

	if depsNode == nil {
		t.Fatal("expected non-nil dependencies node")
	}
	if depsNode.Kind != yaml.SequenceNode {
		t.Errorf("expected SequenceNode, got %v", depsNode.Kind)
	}

	// Call again - should return the same node
	depsNode2 := EnsureDependenciesNode(root)
	if depsNode2 == nil {
		t.Fatal("expected non-nil dependencies node on second call")
	}
}

func TestCommentPreservationRoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kbox-yaml-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a YAML with comments
	content := `# My awesome app config
# Last updated: 2024-01-15
apiVersion: kbox.dev/v1
kind: App
metadata:
  name: myapp
spec:
  # Docker image
  image: myapp:v1
  port: 8080

  # Database for user data
  dependencies:
    - type: postgres
      storage: 10Gi
`
	configPath := filepath.Join(tmpDir, "kbox.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Load, modify, save
	node, err := LoadYAMLWithComments(configPath)
	if err != nil {
		t.Fatal(err)
	}

	root := GetRootDocument(node)
	depsNode := EnsureDependenciesNode(root)

	// Add redis
	redisDep := &DependencyConfig{Type: "redis"}
	AddToSequence(depsNode, DependencyToNode(redisDep))

	// Save
	if err := SaveYAMLWithComments(configPath, node); err != nil {
		t.Fatal(err)
	}

	// Read back and verify
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	savedStr := string(saved)

	// Verify comments are preserved
	if !strings.Contains(savedStr, "# My awesome app config") {
		t.Error("top-level comment not preserved")
	}
	if !strings.Contains(savedStr, "# Last updated: 2024-01-15") {
		t.Error("second top-level comment not preserved")
	}
	if !strings.Contains(savedStr, "# Docker image") {
		t.Error("inline comment not preserved")
	}
	if !strings.Contains(savedStr, "# Database for user data") {
		t.Error("dependencies comment not preserved")
	}

	// Verify new dependency was added
	if !strings.Contains(savedStr, "type: redis") {
		t.Error("redis dependency not added")
	}

	// Verify original dependency still exists
	if !strings.Contains(savedStr, "type: postgres") {
		t.Error("postgres dependency should still exist")
	}
}
