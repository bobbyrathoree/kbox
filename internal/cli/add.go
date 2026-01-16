package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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

	// Find config file path
	loader := config.NewLoader(".")
	configPath, err := loader.FindConfigFile()
	if err != nil {
		return fmt.Errorf("failed to find kbox.yaml: %w\n  → Run 'kbox init' to create one first", err)
	}

	// Load YAML preserving comments
	node, err := config.LoadYAMLWithComments(configPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", configPath, err)
	}

	// Navigate to spec.dependencies (create if needed)
	root := config.GetRootDocument(node)
	depsNode := config.EnsureDependenciesNode(root)

	// Check if dependency already exists
	if config.SequenceContains(depsNode, "type", depType) {
		return fmt.Errorf("%s is already configured\n  → Edit kbox.yaml to modify or use 'kbox remove %s' first", depType, depType)
	}

	// Create and add new dependency node
	newDep := &config.DependencyConfig{
		Type:    depType,
		Version: version,
		Storage: storage,
	}
	config.AddToSequence(depsNode, config.DependencyToNode(newDep))

	// Save YAML preserving comments
	if err := config.SaveYAMLWithComments(configPath, node); err != nil {
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

	// Find config file path
	loader := config.NewLoader(".")
	configPath, err := loader.FindConfigFile()
	if err != nil {
		return fmt.Errorf("failed to find kbox.yaml: %w", err)
	}

	// Load YAML preserving comments
	node, err := config.LoadYAMLWithComments(configPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", configPath, err)
	}

	// Navigate to spec.dependencies
	root := config.GetRootDocument(node)
	specNode := config.FindMapKey(root, "spec")
	if specNode == nil {
		return fmt.Errorf("%s is not configured as a dependency", depType)
	}

	depsNode := config.FindMapKey(specNode, "dependencies")
	if depsNode == nil {
		return fmt.Errorf("%s is not configured as a dependency", depType)
	}

	// Remove the dependency
	if !config.RemoveFromSequence(depsNode, "type", depType) {
		return fmt.Errorf("%s is not configured as a dependency", depType)
	}

	// Save YAML preserving comments
	if err := config.SaveYAMLWithComments(configPath, node); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}

	// Load config to get app name for output message
	cfg, _ := loader.Load()
	appName := "myapp"
	if cfg != nil {
		appName = cfg.Metadata.Name
	}

	fmt.Printf("Removed %s from %s\n", depType, configPath)
	fmt.Println()
	fmt.Printf("Note: Deployed resources are not automatically removed.\n")
	fmt.Printf("  → Run 'kbox deploy' to update the deployment\n")
	fmt.Printf("  → Or delete manually: kubectl delete statefulset %s-%s\n", appName, depType)

	return nil
}

func init() {
	addCmd.Flags().String("storage", "", "Storage size (e.g., 5Gi)")

	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(removeCmd)
}
