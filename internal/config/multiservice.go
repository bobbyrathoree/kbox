package config

import (
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

// IsMultiService checks if a kbox.yaml file defines a multi-service app
func IsMultiService(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return IsMultiServiceData(data)
}

// IsMultiServiceData checks if YAML data defines a multi-service app
func IsMultiServiceData(data []byte) (bool, error) {
	var header struct {
		Kind string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return false, err
	}
	return header.Kind == MultiAppKind, nil
}

// LoadMultiService loads a multi-service configuration from a file
func LoadMultiService(path string) (*MultiServiceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return ParseMultiService(data)
}

// ParseMultiService parses multi-service config from YAML data
func ParseMultiService(data []byte) (*MultiServiceConfig, error) {
	var config MultiServiceConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Apply defaults
	config.WithDefaults()

	return &config, nil
}

// WithDefaults applies default values to multi-service config
func (c *MultiServiceConfig) WithDefaults() *MultiServiceConfig {
	if c.APIVersion == "" {
		c.APIVersion = DefaultAPIVersion
	}
	if c.Kind == "" {
		c.Kind = MultiAppKind
	}

	// Apply defaults to each service
	for name, svc := range c.Services {
		if svc.Port == 0 {
			svc.Port = DefaultPort
		}
		if svc.Replicas == 0 {
			svc.Replicas = DefaultReplicas
		}
		c.Services[name] = svc
	}

	return c
}

// Validate validates the multi-service configuration
func (c *MultiServiceConfig) Validate() error {
	var errs ValidationErrors

	// Validate metadata
	if c.Metadata.Name == "" {
		errs = append(errs, ValidationError{
			Field:   "metadata.name",
			Message: "required",
		})
	} else if !IsValidName(c.Metadata.Name) {
		errs = append(errs, ValidationError{
			Field:   "metadata.name",
			Message: "must be lowercase alphanumeric with hyphens, max 63 chars",
		})
	}

	// Must have at least one service
	if len(c.Services) == 0 {
		errs = append(errs, ValidationError{
			Field:   "services",
			Message: "at least one service is required",
		})
	}

	// Validate each service
	serviceNames := make(map[string]bool)
	for name, svc := range c.Services {
		serviceNames[name] = true

		// Validate service name
		if !IsValidName(name) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("services.%s", name),
				Message: "service name must be lowercase alphanumeric with hyphens",
			})
		}

		// Must have image or build
		if svc.Image == "" && svc.Build == nil {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("services.%s.image", name),
				Message: "image or build required",
			})
		}

		// Validate port
		if svc.Port < 0 || svc.Port > 65535 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("services.%s.port", name),
				Message: "must be between 0 and 65535",
			})
		}

		// Validate replicas
		if svc.Replicas < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("services.%s.replicas", name),
				Message: "must be non-negative",
			})
		}
	}

	// Validate dependsOn references
	for name, svc := range c.Services {
		for _, dep := range svc.DependsOn {
			if !serviceNames[dep] {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("services.%s.dependsOn", name),
					Message: fmt.Sprintf("unknown service %q", dep),
				})
			}
			if dep == name {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("services.%s.dependsOn", name),
					Message: "service cannot depend on itself",
				})
			}
		}
	}

	// Check for circular dependencies
	if err := checkCircularDeps(c.Services); err != nil {
		errs = append(errs, ValidationError{
			Field:   "services.dependsOn",
			Message: err.Error(),
		})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// checkCircularDeps checks for circular dependencies using DFS
func checkCircularDeps(services map[string]ServiceSpec) error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var dfs func(name string) error
	dfs = func(name string) error {
		visited[name] = true
		recStack[name] = true

		svc, ok := services[name]
		if !ok {
			return nil
		}

		for _, dep := range svc.DependsOn {
			if !visited[dep] {
				if err := dfs(dep); err != nil {
					return err
				}
			} else if recStack[dep] {
				return fmt.Errorf("circular dependency: %s -> %s", name, dep)
			}
		}

		recStack[name] = false
		return nil
	}

	for name := range services {
		if !visited[name] {
			if err := dfs(name); err != nil {
				return err
			}
		}
	}

	return nil
}

// ForEnvironment returns a config merged with environment-specific overrides
func (c *MultiServiceConfig) ForEnvironment(env string) *MultiServiceConfig {
	if env == "" || c.Environments == nil {
		return c
	}

	override, ok := c.Environments[env]
	if !ok {
		return c
	}

	// Create a deep copy of the config
	result := *c
	result.Services = make(map[string]ServiceSpec)
	for name, svc := range c.Services {
		result.Services[name] = svc
	}

	// Apply per-service overrides
	if override.Services != nil {
		for name, svcOverride := range override.Services {
			svc, ok := result.Services[name]
			if !ok {
				continue // Skip unknown services
			}

			// Apply overrides
			if svcOverride.Replicas != nil {
				svc.Replicas = *svcOverride.Replicas
			}
			if svcOverride.Image != "" {
				svc.Image = svcOverride.Image
			}
			if svcOverride.Resources != nil {
				svc.Resources = svcOverride.Resources
			}

			// Merge env vars
			if len(svcOverride.Env) > 0 {
				if svc.Env == nil {
					svc.Env = make(map[string]string)
				}
				for k, v := range svcOverride.Env {
					svc.Env[k] = v
				}
			}

			result.Services[name] = svc
		}
	}

	return &result
}

// ToAppConfig converts a single service from MultiServiceConfig to AppConfig
// This is useful for rendering individual services
func (c *MultiServiceConfig) ToAppConfig(serviceName string) (*AppConfig, error) {
	svc, ok := c.Services[serviceName]
	if !ok {
		return nil, fmt.Errorf("service %q not found", serviceName)
	}

	return &AppConfig{
		APIVersion: c.APIVersion,
		Kind:       DefaultKind,
		Metadata: Metadata{
			Name:      fmt.Sprintf("%s-%s", c.Metadata.Name, serviceName),
			Namespace: c.Metadata.Namespace,
			Labels:    c.Metadata.Labels,
		},
		Spec: AppSpec{
			Image:       svc.Image,
			Build:       svc.Build,
			Port:        svc.Port,
			Replicas:    svc.Replicas,
			Env:         svc.Env,
			HealthCheck: svc.HealthCheck,
			Resources:   svc.Resources,
			Command:     svc.Command,
			Args:        svc.Args,
			Service:     svc.Service,
		},
	}, nil
}

// ServiceOrder returns services in dependency order (dependencies first)
func (c *MultiServiceConfig) ServiceOrder() []string {
	return topologicalSort(c.Services)
}

// topologicalSort returns services sorted by dependencies (Kahn's algorithm)
func topologicalSort(services map[string]ServiceSpec) []string {
	// Build in-degree map
	inDegree := make(map[string]int)
	for name := range services {
		inDegree[name] = 0
	}
	for _, svc := range services {
		for _, dep := range svc.DependsOn {
			inDegree[dep]++ // dep is depended upon
		}
	}

	// Find nodes with no incoming edges (no one depends on them)
	// But we want the reverse - things that have no dependencies go first
	// Rebuild: count how many dependencies each service has
	depCount := make(map[string]int)
	for name, svc := range services {
		depCount[name] = len(svc.DependsOn)
	}

	// Queue services with no dependencies
	var queue []string
	for name := range services {
		if depCount[name] == 0 {
			queue = append(queue, name)
		}
	}

	// Build reverse adjacency list (who depends on me)
	dependents := make(map[string][]string)
	for name, svc := range services {
		for _, dep := range svc.DependsOn {
			dependents[dep] = append(dependents[dep], name)
		}
	}

	var result []string
	for len(queue) > 0 {
		// Pop
		name := queue[0]
		queue = queue[1:]
		result = append(result, name)

		// Reduce dependency count for dependents
		for _, dependent := range dependents[name] {
			depCount[dependent]--
			if depCount[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	return result
}
