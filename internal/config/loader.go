package config

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

const (
	// DefaultConfigFile is the default config filename
	DefaultConfigFile = "kbox.yaml"

	// AlternateConfigFile is an alternate config filename
	AlternateConfigFile = "kbox.yml"
)

// Loader handles loading and parsing kbox configuration
type Loader struct {
	workDir string
}

// NewLoader creates a new config loader
func NewLoader(workDir string) *Loader {
	if workDir == "" {
		workDir = "."
	}
	return &Loader{workDir: workDir}
}

// Load loads the kbox config from the working directory
func (l *Loader) Load() (*AppConfig, error) {
	path, err := l.FindConfigFile()
	if err != nil {
		return nil, err
	}
	return l.LoadFile(path)
}

// LoadFile loads config from a specific path
func (l *Loader) LoadFile(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config AppConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w\n  → Check YAML syntax at https://yaml.org/spec/", err)
	}

	// Apply defaults
	config.WithDefaults()

	// Validate
	if err := Validate(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

// FindConfigFile finds the config file in the working directory
func (l *Loader) FindConfigFile() (string, error) {
	// Try kbox.yaml first
	path := filepath.Join(l.workDir, DefaultConfigFile)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Try kbox.yml
	path = filepath.Join(l.workDir, AlternateConfigFile)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("no %s or %s found in %s", DefaultConfigFile, AlternateConfigFile, l.workDir)
}

// HasConfigFile checks if a config file exists
func (l *Loader) HasConfigFile() bool {
	_, err := l.FindConfigFile()
	return err == nil
}

// LoadOrDefault loads config if it exists, or returns a default config
func (l *Loader) LoadOrDefault(name string) (*AppConfig, error) {
	if l.HasConfigFile() {
		return l.Load()
	}
	return NewDefaultConfig(name), nil
}

// IsMultiService checks if the config file is a multi-service config
func (l *Loader) IsMultiService() (bool, error) {
	path, err := l.FindConfigFile()
	if err != nil {
		return false, err
	}
	return IsMultiService(path)
}

// LoadMultiService loads the config as a multi-service config
func (l *Loader) LoadMultiService() (*MultiServiceConfig, error) {
	path, err := l.FindConfigFile()
	if err != nil {
		return nil, err
	}
	return l.LoadMultiServiceFile(path)
}

// LoadMultiServiceFile loads multi-service config from a specific path
func (l *Loader) LoadMultiServiceFile(path string) (*MultiServiceConfig, error) {
	cfg, err := LoadMultiService(path)
	if err != nil {
		return nil, err
	}

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// InferFromDockerfile attempts to infer config from a Dockerfile
func InferFromDockerfile(dir string) (*AppConfig, error) {
	dockerfilePath := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err != nil {
		return nil, fmt.Errorf("no Dockerfile found in %s", dir)
	}

	// Get app name from directory
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	name := filepath.Base(absDir)
	if name == "." || name == "/" {
		name = "app"
	}

	// Read Dockerfile to find EXPOSE port
	data, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Dockerfile: %w", err)
	}

	port := parseExposedPort(string(data))
	if port == 0 {
		port = DefaultPort
	}

	config := NewDefaultConfig(name)
	config.Spec.Port = port
	config.Spec.Build = &BuildConfig{
		Context:    ".",
		Dockerfile: "Dockerfile",
	}

	return config, nil
}

// parseExposedPort extracts the last EXPOSE port from a Dockerfile.
// Returns the last EXPOSE to support multi-stage builds where the final
// stage has the runtime port (e.g., builder stage has 8080, final has 3000).
func parseExposedPort(dockerfile string) int {
	var lastPort int
	lines := splitLines(dockerfile)
	for _, line := range lines {
		line = trimSpace(line)
		if len(line) > 7 && (line[:6] == "EXPOSE" || line[:6] == "expose") {
			// Parse port number
			portStr := trimSpace(line[6:])
			// Handle "EXPOSE 8080/tcp" format
			for i, c := range portStr {
				if c == '/' || c == ' ' {
					portStr = portStr[:i]
					break
				}
			}
			// Parse as int
			var port int
			for _, c := range portStr {
				if c >= '0' && c <= '9' {
					port = port*10 + int(c-'0')
				} else {
					break
				}
			}
			if port > 0 {
				lastPort = port // Keep overwriting to get last EXPOSE
			}
		}
	}
	return lastPort
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
