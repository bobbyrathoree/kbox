package render

import (
	"bytes"
	"encoding/json"
	"io"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

// ToYAML converts a bundle to YAML output
func (b *Bundle) ToYAML(w io.Writer) error {
	objects := b.AllObjects()

	for i, obj := range objects {
		if i > 0 {
			// Separate documents with ---
			if _, err := w.Write([]byte("---\n")); err != nil {
				return err
			}
		}

		if err := writeObjectYAML(w, obj); err != nil {
			return err
		}
	}

	return nil
}

func writeObjectYAML(w io.Writer, obj runtime.Object) error {
	// Marshal directly to YAML - sigs.k8s.io/yaml handles k8s objects
	yamlBytes, err := yaml.Marshal(obj)
	if err != nil {
		return err
	}

	_, err = w.Write(yamlBytes)
	return err
}

// ObjectToYAML converts a single object to YAML string
func ObjectToYAML(obj runtime.Object) (string, error) {
	var buf bytes.Buffer
	if err := writeObjectYAML(&buf, obj); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ToJSON converts a bundle to JSON output (array of objects)
func (b *Bundle) ToJSON(w io.Writer) error {
	objects := b.AllObjects()

	// Convert to JSON-friendly representation
	var jsonObjects []map[string]interface{}
	for _, obj := range objects {
		// Marshal to JSON first, then unmarshal to get clean map
		jsonBytes, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		var objMap map[string]interface{}
		if err := json.Unmarshal(jsonBytes, &objMap); err != nil {
			return err
		}
		jsonObjects = append(jsonObjects, objMap)
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]interface{}{
		"objects": jsonObjects,
	})
}
