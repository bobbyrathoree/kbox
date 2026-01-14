package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/dependencies"
)

var addCmd = &cobra.Command{
	Use:   "add <dependency>",
	Short: "Add a dependency to the app",
	Long: `Add a managed dependency like postgres, redis, or mongodb.

This modifies your kbox.yaml to include the dependency, which will
be deployed as a StatefulSet with persistent storage.

When deployed, kbox automatically injects connection environment
variables (DATABASE_URL, REDIS_URL, etc.) into your app.

Supported dependencies:
  - postgres (or postgres:15)
  - redis (or redis:7)
  - mongodb (or mongodb:6)
  - mysql (or mysql:8)

Examples:
  kbox add postgres           # Add PostgreSQL 15 (default)
  kbox add postgres:14        # Add specific version
  kbox add redis              # Add Redis
  kbox add mongodb            # Add MongoDB`,
	Args: cobra.ExactArgs(1),
	RunE: runAdd,
}

func runAdd(cmd *cobra.Command, args []string) error {
	storage, _ := cmd.Flags().GetString("storage")

	// Parse dependency spec (e.g., "postgres:15" or "postgres")
	depSpec := args[0]
	depType, version := parseDependencySpec(depSpec)

	// Validate dependency type
	if !dependencies.IsSupported(depType) {
		return fmt.Errorf("unsupported dependency: %s\n  → Supported: %v", depType, dependencies.SupportedTypes())
	}

	// Load existing config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w\n  → Run 'kbox init' to create one first", err)
	}

	// Check if dependency already exists
	for _, dep := range cfg.Spec.Dependencies {
		if dep.Type == depType {
			return fmt.Errorf("%s is already configured\n  → Edit kbox.yaml to modify or use 'kbox remove %s' first", depType, depType)
		}
	}

	// Add dependency
	newDep := config.DependencyConfig{
		Type:    depType,
		Version: version,
		Storage: storage,
	}
	cfg.Spec.Dependencies = append(cfg.Spec.Dependencies, newDep)

	// Find and update kbox.yaml
	configPath, err := loader.FindConfigFile()
	if err != nil {
		return err
	}

	// Marshal and write
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate YAML: %w", err)
	}

	if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}

	// Get template for info display
	template, _ := dependencies.Get(depType)
	imageVersion := version
	if imageVersion == "" {
		imageVersion = template.DefaultVersion
	}

	fmt.Printf("Added %s:%s to %s\n", depType, imageVersion, configPath)
	fmt.Println()
	fmt.Println("When deployed, the following env vars will be injected into your app:")
	for k := range template.EnvVars {
		fmt.Printf("  %s\n", k)
	}
	fmt.Println()
	fmt.Printf("  → Run 'kbox deploy' to deploy with %s\n", depType)
	fmt.Printf("  → Run 'kbox render' to preview generated manifests\n")

	return nil
}

// parseDependencySpec parses "postgres:15" into ("postgres", "15")
func parseDependencySpec(spec string) (string, string) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) == 2 {
		return strings.ToLower(parts[0]), parts[1]
	}
	return strings.ToLower(parts[0]), ""
}

var removeCmd = &cobra.Command{
	Use:   "remove <dependency>",
	Short: "Remove a dependency from the app",
	Long: `Remove a managed dependency from kbox.yaml.

Note: This only removes the dependency from the configuration.
To remove deployed resources, run 'kbox deploy' after removal,
or manually delete them with kubectl.

Examples:
  kbox remove postgres
  kbox remove redis`,
	Args: cobra.ExactArgs(1),
	RunE: runRemove,
}

func runRemove(cmd *cobra.Command, args []string) error {
	depType := strings.ToLower(args[0])

	// Load existing config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Find and remove dependency
	found := false
	newDeps := make([]config.DependencyConfig, 0, len(cfg.Spec.Dependencies))
	for _, dep := range cfg.Spec.Dependencies {
		if dep.Type == depType {
			found = true
			continue
		}
		newDeps = append(newDeps, dep)
	}

	if !found {
		return fmt.Errorf("%s is not configured as a dependency", depType)
	}

	cfg.Spec.Dependencies = newDeps

	// Find and update kbox.yaml
	configPath, err := loader.FindConfigFile()
	if err != nil {
		return err
	}

	// Marshal and write
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate YAML: %w", err)
	}

	if err := os.WriteFile(configPath, yamlBytes, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}

	fmt.Printf("Removed %s from %s\n", depType, configPath)
	fmt.Println()
	fmt.Printf("Note: Deployed resources are not automatically removed.\n")
	fmt.Printf("  → Run 'kbox deploy' to update the deployment\n")
	fmt.Printf("  → Or delete manually: kubectl delete statefulset %s-%s\n", cfg.Metadata.Name, depType)

	return nil
}

func init() {
	addCmd.Flags().String("storage", "", "Storage size (e.g., 5Gi)")

	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(removeCmd)
}
