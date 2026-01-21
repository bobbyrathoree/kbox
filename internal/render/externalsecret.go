package render

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RenderExternalSecret creates an ExternalSecret CRD for External Secrets Operator
// Returns nil if external secrets are not configured or if configuration is invalid
func (r *Renderer) RenderExternalSecret() *unstructured.Unstructured {
	if r.config.Spec.Secrets == nil || r.config.Spec.Secrets.External == nil {
		return nil
	}

	cfg := r.config.Spec.Secrets.External

	// Skip if no data or dataFrom is specified (nothing to sync)
	if len(cfg.Data) == 0 && len(cfg.DataFrom) == 0 {
		return nil
	}

	// Skip if storeRef.name is not specified (invalid configuration)
	if cfg.StoreRef.Name == "" {
		return nil
	}

	// Defaults
	refreshInterval := cfg.RefreshInterval
	if refreshInterval == "" {
		refreshInterval = "1h"
	}

	storeKind := cfg.StoreRef.Kind
	if storeKind == "" {
		storeKind = "ClusterSecretStore"
	}

	// Build the target secret name
	targetSecretName := r.config.Metadata.Name + "-external-secrets"

	// Build data array for individual key mappings
	var data []interface{}
	for _, d := range cfg.Data {
		// Build remoteRef first to avoid type assertion
		remoteRef := map[string]interface{}{
			"key": d.RemoteKey,
		}
		// Add property if specified
		if d.Property != "" {
			remoteRef["property"] = d.Property
		}

		dataEntry := map[string]interface{}{
			"secretKey": d.EnvVar,
			"remoteRef": remoteRef,
		}

		data = append(data, dataEntry)
	}

	// Build dataFrom array for extracting all keys
	var dataFrom []interface{}
	for _, df := range cfg.DataFrom {
		dataFrom = append(dataFrom, map[string]interface{}{
			"extract": map[string]interface{}{
				"key": df.RemoteKey,
			},
		})
	}

	// Build ExternalSecret spec
	spec := map[string]interface{}{
		"refreshInterval": refreshInterval,
		"secretStoreRef": map[string]interface{}{
			"name": cfg.StoreRef.Name,
			"kind": storeKind,
		},
		"target": map[string]interface{}{
			"name":           targetSecretName,
			"creationPolicy": "Owner",
		},
	}

	// Add data if any
	if len(data) > 0 {
		spec["data"] = data
	}

	// Add dataFrom if any
	if len(dataFrom) > 0 {
		spec["dataFrom"] = dataFrom
	}

	// Build ExternalSecret using unstructured to avoid external-secrets dependency
	es := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "external-secrets.io/v1beta1",
			"kind":       "ExternalSecret",
			"metadata": map[string]interface{}{
				"name":      r.config.Metadata.Name + "-external",
				"namespace": r.Namespace(),
				"labels":    toInterfaceMap(r.Labels()),
			},
			"spec": spec,
		},
	}

	return es
}

// ExternalSecretTargetName returns the name of the target secret created by ExternalSecret
// This is used by deployment to reference the secret in envFrom
func (r *Renderer) ExternalSecretTargetName() string {
	if r.config.Spec.Secrets == nil || r.config.Spec.Secrets.External == nil {
		return ""
	}
	return r.config.Metadata.Name + "-external-secrets"
}
