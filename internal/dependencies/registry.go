package dependencies

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// Template defines how to create a dependency
type Template struct {
	// Image base name
	Image string

	// DefaultVersion when not specified
	DefaultVersion string

	// DefaultPort for the service
	DefaultPort int32

	// DefaultStorage size
	DefaultStorage string

	// EnvVars to inject into the app
	// Supports {{.Service}} and {{.Password}} placeholders
	EnvVars map[string]string

	// SecretKeys are environment variables for the dependency pod
	// that need auto-generated passwords
	SecretKeys []string

	// HealthCheck command for readiness probe
	HealthCheck []string

	// ConnectCommand for kbox db connect
	ConnectCommand []string
}

// Registry maps dependency types to their templates
var Registry = map[string]Template{
	"postgres": {
		Image:          "postgres",
		DefaultVersion: "15-alpine",
		DefaultPort:    5432,
		DefaultStorage: "1Gi",
		EnvVars: map[string]string{
			"DATABASE_URL": "postgres://postgres:{{.Password}}@{{.Service}}:5432/postgres",
			"PGHOST":       "{{.Service}}",
			"PGPORT":       "5432",
			"PGUSER":       "postgres",
			"PGPASSWORD":   "{{.Password}}",
			"PGDATABASE":   "postgres",
		},
		SecretKeys:     []string{"POSTGRES_PASSWORD"},
		HealthCheck:    []string{"pg_isready", "-U", "postgres"},
		ConnectCommand: []string{"psql", "-U", "postgres"},
	},
	"redis": {
		Image:          "redis",
		DefaultVersion: "7-alpine",
		DefaultPort:    6379,
		DefaultStorage: "1Gi",
		EnvVars: map[string]string{
			"REDIS_URL":  "redis://{{.Service}}:6379",
			"REDIS_HOST": "{{.Service}}",
			"REDIS_PORT": "6379",
		},
		SecretKeys:     nil, // Redis doesn't require password by default
		HealthCheck:    []string{"redis-cli", "ping"},
		ConnectCommand: []string{"redis-cli"},
	},
	"mongodb": {
		Image:          "mongo",
		DefaultVersion: "6",
		DefaultPort:    27017,
		DefaultStorage: "1Gi",
		EnvVars: map[string]string{
			"MONGODB_URL":  "mongodb://{{.Service}}:27017",
			"MONGODB_HOST": "{{.Service}}",
			"MONGODB_PORT": "27017",
		},
		SecretKeys:     nil,
		HealthCheck:    []string{"mongosh", "--eval", "db.adminCommand('ping')"},
		ConnectCommand: []string{"mongosh"},
	},
	"mysql": {
		Image:          "mysql",
		DefaultVersion: "8",
		DefaultPort:    3306,
		DefaultStorage: "1Gi",
		EnvVars: map[string]string{
			"DATABASE_URL":  "mysql://root:{{.Password}}@{{.Service}}:3306/mysql",
			"MYSQL_HOST":    "{{.Service}}",
			"MYSQL_PORT":    "3306",
			"MYSQL_USER":    "root",
			"MYSQL_PASSWORD": "{{.Password}}",
		},
		SecretKeys:     []string{"MYSQL_ROOT_PASSWORD"},
		HealthCheck:    []string{"mysqladmin", "ping", "-h", "localhost"},
		ConnectCommand: []string{"mysql", "-u", "root", "-p"},
	},
}

// Get returns a template for the given type
func Get(depType string) (Template, bool) {
	t, ok := Registry[strings.ToLower(depType)]
	return t, ok
}

// SupportedTypes returns all supported dependency types
func SupportedTypes() []string {
	types := make([]string, 0, len(Registry))
	for t := range Registry {
		types = append(types, t)
	}
	return types
}

// IsSupported checks if a dependency type is supported
func IsSupported(depType string) bool {
	_, ok := Registry[strings.ToLower(depType)]
	return ok
}

// GeneratePassword creates a random password for secrets
func GeneratePassword() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// RenderEnvVars renders environment variable values with placeholders replaced
func RenderEnvVars(template Template, serviceName, password string) map[string]string {
	result := make(map[string]string)
	for k, v := range template.EnvVars {
		rendered := v
		rendered = strings.ReplaceAll(rendered, "{{.Service}}", serviceName)
		rendered = strings.ReplaceAll(rendered, "{{.Password}}", password)
		result[k] = rendered
	}
	return result
}

// EnvVarSecretInfo holds information about how an env var should reference a secret
type EnvVarSecretInfo struct {
	SecretName string
	SecretKey  string
}

// RenderEnvVarsWithSecretRefs separates env vars into those with plaintext values
// and those that should use secretKeyRef to avoid exposing passwords in plain text.
// It also returns the secret data that should be added to the dependency secret.
func RenderEnvVarsWithSecretRefs(template Template, serviceName, secretName, password string) (plainEnvVars map[string]string, secretEnvVars map[string]EnvVarSecretInfo, secretData map[string]string) {
	plainEnvVars = make(map[string]string)
	secretEnvVars = make(map[string]EnvVarSecretInfo)
	secretData = make(map[string]string)

	for k, v := range template.EnvVars {
		if strings.Contains(v, "{{.Password}}") {
			// This env var contains a password - store the rendered value in the secret
			// and reference it with secretKeyRef
			rendered := v
			rendered = strings.ReplaceAll(rendered, "{{.Service}}", serviceName)
			rendered = strings.ReplaceAll(rendered, "{{.Password}}", password)

			// Store in secret data with the env var name as the key
			secretData[k] = rendered

			// App should reference this key from the secret
			secretEnvVars[k] = EnvVarSecretInfo{
				SecretName: secretName,
				SecretKey:  k,
			}
		} else {
			// No password - render as plaintext
			rendered := strings.ReplaceAll(v, "{{.Service}}", serviceName)
			plainEnvVars[k] = rendered
		}
	}
	return plainEnvVars, secretEnvVars, secretData
}

// ImageWithVersion returns the full image reference
func ImageWithVersion(template Template, version string) string {
	if version == "" {
		version = template.DefaultVersion
	}
	return fmt.Sprintf("%s:%s", template.Image, version)
}
