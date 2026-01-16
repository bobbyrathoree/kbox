package config

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

// ValidationError represents a config validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return fmt.Sprintf("validation failed:\n  - %s", strings.Join(msgs, "\n  - "))
}

// Validate validates an AppConfig
func Validate(config *AppConfig) error {
	var errs ValidationErrors

	// Check API version
	if config.APIVersion != "" && config.APIVersion != DefaultAPIVersion {
		errs = append(errs, ValidationError{
			Field:   "apiVersion",
			Message: fmt.Sprintf("unsupported version %q, expected %q", config.APIVersion, DefaultAPIVersion),
		})
	}

	// Check kind
	if config.Kind != "" && config.Kind != DefaultKind {
		errs = append(errs, ValidationError{
			Field:   "kind",
			Message: fmt.Sprintf("unsupported kind %q, expected %q", config.Kind, DefaultKind),
		})
	}

	// Check metadata.name
	if config.Metadata.Name == "" {
		errs = append(errs, ValidationError{
			Field:   "metadata.name",
			Message: "required",
		})
	} else if !IsValidName(config.Metadata.Name) {
		errs = append(errs, ValidationError{
			Field:   "metadata.name",
			Message: "must be lowercase alphanumeric with hyphens, max 63 chars",
		})
	}

	// Check image or build
	if config.Spec.Image == "" && config.Spec.Build == nil {
		errs = append(errs, ValidationError{
			Field:   "spec.image",
			Message: "either image or build configuration is required",
		})
	}

	// Check port
	if config.Spec.Port < 0 || config.Spec.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "spec.port",
			Message: "must be between 0 and 65535",
		})
	}

	// Check replicas
	if config.Spec.Replicas < 0 {
		errs = append(errs, ValidationError{
			Field:   "spec.replicas",
			Message: "must be non-negative",
		})
	}

	// Check service type
	if config.Spec.Service != nil && config.Spec.Service.Type != "" {
		validTypes := map[string]bool{
			"ClusterIP":    true,
			"NodePort":     true,
			"LoadBalancer": true,
		}
		if !validTypes[config.Spec.Service.Type] {
			errs = append(errs, ValidationError{
				Field:   "spec.service.type",
				Message: "must be ClusterIP, NodePort, or LoadBalancer",
			})
		}
	}

	// Check ingress
	if config.Spec.Ingress != nil && config.Spec.Ingress.Enabled {
		if config.Spec.Ingress.Host == "" {
			errs = append(errs, ValidationError{
				Field:   "spec.ingress.host",
				Message: "required when ingress is enabled",
			})
		}
	}

	// Validate environments
	for envName, env := range config.Environments {
		if env.Replicas != nil && *env.Replicas < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("environments.%s.replicas", envName),
				Message: "must be non-negative",
			})
		}
	}

	// Validate resource quantities
	if config.Spec.Resources != nil {
		res := config.Spec.Resources
		quantityFields := []struct {
			val   string
			field string
		}{
			{res.Memory, "spec.resources.memory"},
			{res.CPU, "spec.resources.cpu"},
			{res.MemoryLimit, "spec.resources.memoryLimit"},
			{res.CPULimit, "spec.resources.cpuLimit"},
		}
		for _, check := range quantityFields {
			if err := validateQuantity(check.val, check.field); err != nil {
				errs = append(errs, *err)
			}
		}

		// Validate request <= limit for memory
		if res.Memory != "" && res.MemoryLimit != "" {
			memReq, errReq := resource.ParseQuantity(res.Memory)
			memLim, errLim := resource.ParseQuantity(res.MemoryLimit)
			if errReq == nil && errLim == nil && memReq.Cmp(memLim) > 0 {
				errs = append(errs, ValidationError{
					Field:   "spec.resources",
					Message: fmt.Sprintf("memory request (%s) exceeds limit (%s)", res.Memory, res.MemoryLimit),
				})
			}
		}

		// Validate request <= limit for CPU
		if res.CPU != "" && res.CPULimit != "" {
			cpuReq, errReq := resource.ParseQuantity(res.CPU)
			cpuLim, errLim := resource.ParseQuantity(res.CPULimit)
			if errReq == nil && errLim == nil && cpuReq.Cmp(cpuLim) > 0 {
				errs = append(errs, ValidationError{
					Field:   "spec.resources",
					Message: fmt.Sprintf("cpu request (%s) exceeds limit (%s)", res.CPU, res.CPULimit),
				})
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validateQuantity validates a Kubernetes resource quantity string
func validateQuantity(value, field string) *ValidationError {
	if value == "" {
		return nil
	}
	if _, err := resource.ParseQuantity(value); err != nil {
		return &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("invalid Kubernetes quantity %q: %v", value, err),
		}
	}
	return nil
}

// ValidateWithWarnings validates an AppConfig and returns warnings for non-critical issues
func ValidateWithWarnings(config *AppConfig) ([]string, error) {
	var warnings []string

	// Check for :latest tag or missing tag (security/reproducibility risk)
	if config.Spec.Image != "" {
		if strings.HasSuffix(config.Spec.Image, ":latest") {
			warnings = append(warnings, "image uses :latest tag - consider pinning to a specific version for reproducibility")
		} else if !strings.Contains(config.Spec.Image, ":") && !strings.Contains(config.Spec.Image, "@") {
			warnings = append(warnings, "image has no tag - will default to :latest, consider pinning to a specific version")
		}
	}

	// Run standard validation
	if err := Validate(config); err != nil {
		return warnings, err
	}

	return warnings, nil
}

// IsValidName checks if a name is a valid Kubernetes name
func IsValidName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}

	// Must start with lowercase letter
	if name[0] < 'a' || name[0] > 'z' {
		return false
	}

	// Must end with alphanumeric
	last := name[len(name)-1]
	if !((last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')) {
		return false
	}

	// Can contain lowercase letters, numbers, and hyphens
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}

	return true
}
