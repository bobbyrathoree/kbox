// Package secrets handles secret loading and management
package secrets

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LoadEnvFile parses a .env file and returns key-value pairs
// Supports:
//   - KEY=value
//   - KEY="quoted value"
//   - KEY='single quoted'
//   - # comments
//   - Empty lines
func LoadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open env file: %w", err)
	}
	defer file.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=value
		key, value, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		result[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read env file: %w", err)
	}

	return result, nil
}

// parseLine parses a single KEY=value line
func parseLine(line string) (string, string, error) {
	// Find the first =
	idx := strings.Index(line, "=")
	if idx == -1 {
		return "", "", fmt.Errorf("invalid format: missing '=' in %q", line)
	}

	key := strings.TrimSpace(line[:idx])
	value := line[idx+1:]

	// Validate key
	if key == "" {
		return "", "", fmt.Errorf("empty key")
	}
	if !isValidEnvKey(key) {
		return "", "", fmt.Errorf("invalid key %q: must be alphanumeric with underscores", key)
	}

	// Handle quoted values
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}

	return key, value, nil
}

// isValidEnvKey checks if a key is a valid environment variable name
func isValidEnvKey(key string) bool {
	if len(key) == 0 {
		return false
	}
	// First char must be letter or underscore
	if !isLetter(key[0]) && key[0] != '_' {
		return false
	}
	// Rest must be alphanumeric or underscore
	for i := 1; i < len(key); i++ {
		if !isAlphanumeric(key[i]) && key[i] != '_' {
			return false
		}
	}
	return true
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAlphanumeric(c byte) bool {
	return isLetter(c) || (c >= '0' && c <= '9')
}

// CreateSecret creates a Kubernetes Secret from key-value pairs
func CreateSecret(name, namespace string, data map[string]string, labels map[string]string) *corev1.Secret {
	// Convert string values to []byte for Secret data
	secretData := make(map[string][]byte)
	for k, v := range data {
		secretData[k] = []byte(v)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: secretData,
	}
}

// LoadAndCreateSecret loads an env file and creates a Secret from it
func LoadAndCreateSecret(envFilePath, name, namespace string, labels map[string]string) (*corev1.Secret, error) {
	data, err := LoadEnvFile(envFilePath)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("env file %s is empty", envFilePath)
	}

	return CreateSecret(name, namespace, data, labels), nil
}

// RedactSecret returns a copy of the secret with values redacted for display
func RedactSecret(secret *corev1.Secret) map[string]string {
	redacted := make(map[string]string)
	for k := range secret.Data {
		redacted[k] = "***REDACTED***"
	}
	return redacted
}
