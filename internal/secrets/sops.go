package secrets

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// LoadSopsFile decrypts a SOPS-encrypted file and returns key-value pairs.
// The file can be YAML or JSON format. SOPS must be installed.
func LoadSopsFile(path string) (map[string]string, error) {
	// Check if sops is available
	if _, err := exec.LookPath("sops"); err != nil {
		return nil, fmt.Errorf("sops not found in PATH\n  → Install sops: https://github.com/getsops/sops\n  → Run 'kbox doctor' to check your setup")
	}

	// Decrypt using sops CLI, output as JSON for easy parsing
	cmd := exec.Command("sops", "-d", "--output-type", "json", path)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := string(exitErr.Stderr)
			if strings.Contains(stderr, "could not decrypt") || strings.Contains(stderr, "key") {
				return nil, fmt.Errorf("failed to decrypt %s: missing decryption key\n  → Ensure your age/GPG key is configured in ~/.sops.yaml or SOPS_AGE_KEY_FILE", path)
			}
			return nil, fmt.Errorf("sops decryption failed: %s", stderr)
		}
		return nil, fmt.Errorf("failed to run sops: %w", err)
	}

	// Parse JSON output
	var data map[string]interface{}
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, fmt.Errorf("failed to parse decrypted output: %w", err)
	}

	// Convert to string map (flatten nested structures)
	result := make(map[string]string)
	flattenMap("", data, result)

	return result, nil
}

// flattenMap recursively flattens a nested map into key-value pairs.
// Nested keys are joined with underscores: {a: {b: "c"}} -> {"a_b": "c"}
func flattenMap(prefix string, data map[string]interface{}, result map[string]string) {
	for key, value := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "_" + key
		}

		switch v := value.(type) {
		case string:
			result[fullKey] = v
		case float64:
			result[fullKey] = fmt.Sprintf("%v", v)
		case bool:
			result[fullKey] = fmt.Sprintf("%v", v)
		case map[string]interface{}:
			// Skip sops metadata
			if key == "sops" {
				continue
			}
			flattenMap(fullKey, v, result)
		case nil:
			result[fullKey] = ""
		default:
			// For arrays or other types, convert to JSON string
			if b, err := json.Marshal(v); err == nil {
				result[fullKey] = string(b)
			}
		}
	}
}

// LoadSopsFiles decrypts multiple SOPS files and merges the results.
// Later files override values from earlier files.
func LoadSopsFiles(paths []string) (map[string]string, error) {
	result := make(map[string]string)

	for _, path := range paths {
		data, err := LoadSopsFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", path, err)
		}

		// Merge (later values override earlier)
		for k, v := range data {
			result[k] = v
		}
	}

	return result, nil
}

// LoadSopsAndCreateSecret loads SOPS files and creates a Kubernetes Secret
func LoadSopsAndCreateSecret(paths []string, name, namespace string, labels map[string]string) (*corev1.Secret, error) {
	data, err := LoadSopsFiles(paths)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("SOPS files are empty or contain only metadata")
	}

	return CreateSecret(name, namespace, data, labels), nil
}
