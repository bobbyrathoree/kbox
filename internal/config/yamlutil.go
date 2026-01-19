package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// LoadYAMLWithComments loads a YAML file preserving the AST structure including comments
func LoadYAMLWithComments(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

// SaveYAMLWithComments writes a YAML node back to file, preserving comments
func SaveYAMLWithComments(path string, node *yaml.Node) error {
	data, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// FindMapKey finds a key in a YAML mapping node and returns its value node
func FindMapKey(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// AddMapKey adds a key-value pair to a mapping node
func AddMapKey(node *yaml.Node, key string, value *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		value,
	)
}

// AddToSequence adds an item to a YAML sequence node
func AddToSequence(seq *yaml.Node, item *yaml.Node) {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return
	}
	seq.Content = append(seq.Content, item)
}

// RemoveFromSequence removes an item from a sequence by matching a field value
// Returns true if an item was removed
func RemoveFromSequence(seq *yaml.Node, field, value string) bool {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return false
	}
	for i := 0; i < len(seq.Content); i++ {
		item := seq.Content[i]
		if item.Kind == yaml.MappingNode {
			for j := 0; j < len(item.Content); j += 2 {
				if item.Content[j].Value == field && item.Content[j+1].Value == value {
					seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
					return true
				}
			}
		}
	}
	return false
}

// SequenceContains checks if a sequence contains an item with the given field value
func SequenceContains(seq *yaml.Node, field, value string) bool {
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return false
	}
	for i := 0; i < len(seq.Content); i++ {
		item := seq.Content[i]
		if item.Kind == yaml.MappingNode {
			for j := 0; j < len(item.Content); j += 2 {
				if item.Content[j].Value == field && item.Content[j+1].Value == value {
					return true
				}
			}
		}
	}
	return false
}

// DependencyToNode converts a DependencyConfig to a YAML node
func DependencyToNode(dep *DependencyConfig) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}

	// Add type field (always required)
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "type"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: dep.Type},
	)

	// Add version field (if specified)
	if dep.Version != "" {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "version"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: dep.Version},
		)
	}

	// Add storage field (if specified)
	if dep.Storage != "" {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "storage"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: dep.Storage},
		)
	}

	return node
}

// GetRootDocument returns the document node's content (handles YAML document wrapper)
func GetRootDocument(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	// yaml.v3 wraps content in a DocumentNode
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

// EnsureDependenciesNode ensures spec.dependencies exists and returns it
// If it doesn't exist, creates it
func EnsureDependenciesNode(root *yaml.Node) *yaml.Node {
	specNode := FindMapKey(root, "spec")
	if specNode == nil {
		// Create spec node if it doesn't exist
		specNode = &yaml.Node{Kind: yaml.MappingNode}
		AddMapKey(root, "spec", specNode)
	}

	depsNode := FindMapKey(specNode, "dependencies")
	if depsNode == nil {
		// Create dependencies array if it doesn't exist
		depsNode = &yaml.Node{Kind: yaml.SequenceNode}
		AddMapKey(specNode, "dependencies", depsNode)
	}

	return depsNode
}

// SetMapKey sets a key-value pair in a mapping node (updates if exists, adds if not)
func SetMapKey(node *yaml.Node, key, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	// Look for existing key
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Value = value
			return
		}
	}
	// Key not found, add it
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value},
	)
}

// RemoveMapKey removes a key from a mapping node
// Returns true if the key was found and removed
func RemoveMapKey(node *yaml.Node, key string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return true
		}
	}
	return false
}

// EnsureEnvNode ensures spec.env exists and returns it
// If it doesn't exist, creates it
func EnsureEnvNode(root *yaml.Node) *yaml.Node {
	specNode := FindMapKey(root, "spec")
	if specNode == nil {
		specNode = &yaml.Node{Kind: yaml.MappingNode}
		AddMapKey(root, "spec", specNode)
	}

	envNode := FindMapKey(specNode, "env")
	if envNode == nil {
		envNode = &yaml.Node{Kind: yaml.MappingNode}
		AddMapKey(specNode, "env", envNode)
	}

	return envNode
}

// EnsureEnvironmentEnvNode ensures environments.<env>.env exists and returns it
func EnsureEnvironmentEnvNode(root *yaml.Node, envName string) *yaml.Node {
	envsNode := FindMapKey(root, "environments")
	if envsNode == nil {
		envsNode = &yaml.Node{Kind: yaml.MappingNode}
		AddMapKey(root, "environments", envsNode)
	}

	targetEnvNode := FindMapKey(envsNode, envName)
	if targetEnvNode == nil {
		targetEnvNode = &yaml.Node{Kind: yaml.MappingNode}
		AddMapKey(envsNode, envName, targetEnvNode)
	}

	envVarsNode := FindMapKey(targetEnvNode, "env")
	if envVarsNode == nil {
		envVarsNode = &yaml.Node{Kind: yaml.MappingNode}
		AddMapKey(targetEnvNode, "env", envVarsNode)
	}

	return envVarsNode
}

// GetMapEntries returns all key-value pairs from a mapping node
func GetMapEntries(node *yaml.Node) map[string]string {
	result := make(map[string]string)
	if node == nil || node.Kind != yaml.MappingNode {
		return result
	}
	for i := 0; i < len(node.Content); i += 2 {
		result[node.Content[i].Value] = node.Content[i+1].Value
	}
	return result
}
