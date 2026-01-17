package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// TemplateType represents a scaffold template type
type TemplateType string

const (
	TemplateAPI    TemplateType = "api"
	TemplateWorker TemplateType = "worker"
	TemplateCron   TemplateType = "cron"
)

// Language represents a programming language
type Language string

const (
	LangGo     Language = "go"
	LangNode   Language = "node"
	LangPython Language = "python"
)

// Config holds the template generation configuration
type Config struct {
	// Name of the project/service
	Name string

	// OutputDir is where to write files (default: ./<Name>)
	OutputDir string

	// Type of template (api, worker, cron)
	Type TemplateType

	// Language/framework
	Lang Language

	// Port number for the service
	Port int

	// Database dependency to include (postgres, redis, none)
	Database string

	// Force overwrite existing files
	Force bool
}

// File represents a file to be generated
type File struct {
	// Path relative to output directory
	Path string

	// Content template
	Content string

	// Description for user feedback
	Description string
}

// Generator generates project scaffolds
type Generator struct {
	config Config
}

// NewGenerator creates a new template generator
func NewGenerator(cfg Config) *Generator {
	if cfg.OutputDir == "" {
		cfg.OutputDir = cfg.Name
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	return &Generator{config: cfg}
}

// Generate creates all files for the template
func (g *Generator) Generate() error {
	// Get files for this template type
	files, err := g.getTemplateFiles()
	if err != nil {
		return err
	}

	// Check if output directory exists
	if _, err := os.Stat(g.config.OutputDir); err == nil && !g.config.Force {
		return fmt.Errorf("directory %q already exists (use --force to overwrite)", g.config.OutputDir)
	}

	// Create output directory
	if err := os.MkdirAll(g.config.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate each file
	for _, file := range files {
		path := filepath.Join(g.config.OutputDir, file.Path)

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", file.Path, err)
		}

		// Check if file exists
		if _, err := os.Stat(path); err == nil && !g.config.Force {
			return fmt.Errorf("file %q already exists (use --force to overwrite)", path)
		}

		// Parse and execute template
		tmpl, err := template.New(file.Path).Parse(file.Content)
		if err != nil {
			return fmt.Errorf("failed to parse template for %s: %w", file.Path, err)
		}

		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("failed to create %s: %w", path, err)
		}

		err = tmpl.Execute(f, g.templateData())
		f.Close()
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}

		fmt.Printf("  Created: %s\n", path)
	}

	return nil
}

// templateData returns the data for template rendering
func (g *Generator) templateData() map[string]interface{} {
	return map[string]interface{}{
		"Name":     g.config.Name,
		"Port":     g.config.Port,
		"Database": g.config.Database,
		"Lang":     string(g.config.Lang),
		"Module":   g.moduleName(),
	}
}

// moduleName generates a Go module name
func (g *Generator) moduleName() string {
	// Use the service name as the module name
	return g.config.Name
}

// getTemplateFiles returns the files for the configured template type
func (g *Generator) getTemplateFiles() ([]File, error) {
	switch g.config.Type {
	case TemplateAPI:
		return g.getAPIFiles()
	case TemplateWorker:
		return g.getWorkerFiles()
	case TemplateCron:
		return g.getCronFiles()
	default:
		return nil, fmt.Errorf("unsupported template type: %s", g.config.Type)
	}
}

// SupportedTypes returns all supported template types
func SupportedTypes() []TemplateType {
	return []TemplateType{TemplateAPI, TemplateWorker, TemplateCron}
}

// SupportedLanguages returns all supported languages
func SupportedLanguages() []Language {
	return []Language{LangGo, LangNode, LangPython}
}

// ValidateType checks if a template type is valid
func ValidateType(t string) (TemplateType, bool) {
	switch strings.ToLower(t) {
	case "api":
		return TemplateAPI, true
	case "worker":
		return TemplateWorker, true
	case "cron":
		return TemplateCron, true
	default:
		return "", false
	}
}

// ValidateLanguage checks if a language is valid
func ValidateLanguage(l string) (Language, bool) {
	switch strings.ToLower(l) {
	case "go", "golang":
		return LangGo, true
	case "node", "nodejs", "js":
		return LangNode, true
	case "python", "py":
		return LangPython, true
	default:
		return "", false
	}
}
