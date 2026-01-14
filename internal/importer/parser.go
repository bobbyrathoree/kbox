package importer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

// K8sResources holds parsed Kubernetes resources
type K8sResources struct {
	Deployments []*appsv1.Deployment
	Services    []*corev1.Service
	ConfigMaps  []*corev1.ConfigMap
	Secrets     []*corev1.Secret
}

// ParseFiles parses Kubernetes YAML files and returns the resources
func ParseFiles(paths []string) (*K8sResources, error) {
	resources := &K8sResources{}

	for _, path := range paths {
		if err := parseFile(path, resources); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", path, err)
		}
	}

	return resources, nil
}

// ParseDirectory parses all YAML files in a directory
func ParseDirectory(dir string) (*K8sResources, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			paths = append(paths, filepath.Join(dir, name))
		}
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("no YAML files found in %s", dir)
	}

	return ParseFiles(paths)
}

func parseFile(path string, resources *K8sResources) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Split by document separator (---)
	docs := splitYAMLDocuments(data)

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer()

	for i, doc := range docs {
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		obj, gvk, err := decoder.Decode(doc, nil, nil)
		if err != nil {
			// Skip invalid documents with a warning
			continue
		}

		if err := addResource(obj, gvk.Kind, resources); err != nil {
			return fmt.Errorf("document %d: %w", i+1, err)
		}
	}

	return nil
}

func splitYAMLDocuments(data []byte) [][]byte {
	// Split on --- but be careful about content
	var docs [][]byte
	separator := []byte("\n---")

	for {
		idx := bytes.Index(data, separator)
		if idx == -1 {
			// No more separators
			docs = append(docs, data)
			break
		}

		docs = append(docs, data[:idx])
		data = data[idx+len(separator):]
	}

	return docs
}

func addResource(obj runtime.Object, kind string, resources *K8sResources) error {
	switch kind {
	case "Deployment":
		dep, ok := obj.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("failed to cast to Deployment")
		}
		resources.Deployments = append(resources.Deployments, dep)

	case "Service":
		svc, ok := obj.(*corev1.Service)
		if !ok {
			return fmt.Errorf("failed to cast to Service")
		}
		resources.Services = append(resources.Services, svc)

	case "ConfigMap":
		cm, ok := obj.(*corev1.ConfigMap)
		if !ok {
			return fmt.Errorf("failed to cast to ConfigMap")
		}
		resources.ConfigMaps = append(resources.ConfigMaps, cm)

	case "Secret":
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("failed to cast to Secret")
		}
		resources.Secrets = append(resources.Secrets, secret)

	// Skip other resource types silently
	default:
		// Unsupported resource type, skip
	}

	return nil
}

// HasDeployment returns true if at least one Deployment was parsed
func (r *K8sResources) HasDeployment() bool {
	return len(r.Deployments) > 0
}

// Summary returns a human-readable summary of parsed resources
func (r *K8sResources) Summary() string {
	var parts []string
	if len(r.Deployments) > 0 {
		parts = append(parts, fmt.Sprintf("%d Deployment(s)", len(r.Deployments)))
	}
	if len(r.Services) > 0 {
		parts = append(parts, fmt.Sprintf("%d Service(s)", len(r.Services)))
	}
	if len(r.ConfigMaps) > 0 {
		parts = append(parts, fmt.Sprintf("%d ConfigMap(s)", len(r.ConfigMaps)))
	}
	if len(r.Secrets) > 0 {
		parts = append(parts, fmt.Sprintf("%d Secret(s)", len(r.Secrets)))
	}

	if len(parts) == 0 {
		return "no supported resources found"
	}

	return strings.Join(parts, ", ")
}
