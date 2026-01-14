package render

import (
	"bytes"
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
