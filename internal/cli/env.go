package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/bobbyrathoree/kbox/internal/config"
)

var envCmd = &cobra.Command{
	Use:   "env [command]",
	Short: "Manage environment variables",
	Long: `Manage environment variables in kbox.yaml.

Changes are saved to kbox.yaml and applied on next deploy.

Commands:
  (none)    List all environment variables
  list      List all environment variables
  set       Set an environment variable
  unset     Remove an environment variable

Examples:
  kbox env                          # List all env vars
  kbox env set DEBUG=true           # Set env var in spec.env
  kbox env set -e prod LOG=warn     # Set for prod environment
  kbox env unset DEBUG              # Remove env var`,
	RunE: runEnvList,
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List environment variables",
	Long: `Display all environment variables defined in kbox.yaml.

Shows base env vars (spec.env) and any environment-specific overrides.`,
	RunE: runEnvList,
}

var envSetCmd = &cobra.Command{
	Use:   "set <KEY=VALUE>",
	Short: "Set environment variable",
	Long: `Set an environment variable in kbox.yaml.

By default, sets the variable in spec.env (applies to all environments).
Use -e to set for a specific environment override.

Examples:
  kbox env set DATABASE_URL=postgres://localhost/db
  kbox env set LOG_LEVEL=debug
  kbox env set -e prod LOG_LEVEL=warn
  kbox env set -e staging REPLICAS=2`,
	Args: cobra.ExactArgs(1),
	RunE: runEnvSet,
}

var envUnsetCmd = &cobra.Command{
	Use:   "unset <KEY>",
	Short: "Remove environment variable",
	Long: `Remove an environment variable from kbox.yaml.

By default, removes from spec.env.
Use -e to remove from a specific environment override.

Examples:
  kbox env unset DEBUG
  kbox env unset -e prod LOG_LEVEL`,
	Args: cobra.ExactArgs(1),
	RunE: runEnvUnset,
}

func init() {
	// Add environment flag to set and unset
	envSetCmd.Flags().StringP("environment", "e", "", "Target environment (staging, prod, etc.)")
	envUnsetCmd.Flags().StringP("environment", "e", "", "Target environment (staging, prod, etc.)")

	envCmd.AddCommand(envListCmd)
	envCmd.AddCommand(envSetCmd)
	envCmd.AddCommand(envUnsetCmd)

	rootCmd.AddCommand(envCmd)
}

func runEnvList(cmd *cobra.Command, args []string) error {
	loader := config.NewLoader(".")
	configPath, err := loader.FindConfigFile()
	if err != nil {
		return fmt.Errorf("no kbox.yaml found\n  Run 'kbox init' to create one")
	}

	// Load config to get structured data
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Print base env vars
	fmt.Println("Environment variables (spec.env):")
	if len(cfg.Spec.Env) == 0 {
		fmt.Println("  (none)")
	} else {
		printEnvVars(cfg.Spec.Env)
	}

	// Print environment-specific overrides
	if len(cfg.Environments) > 0 {
		fmt.Println()
		fmt.Println("Environment overrides:")

		// Sort environment names for consistent output
		envNames := make([]string, 0, len(cfg.Environments))
		for name := range cfg.Environments {
			envNames = append(envNames, name)
		}
		sort.Strings(envNames)

		for _, envName := range envNames {
			envOverride := cfg.Environments[envName]
			if len(envOverride.Env) > 0 {
				fmt.Printf("  %s:\n", envName)
				printEnvVarsIndented(envOverride.Env, "    ")
			}
		}
	}

	fmt.Println()
	fmt.Printf("Config: %s\n", configPath)

	return nil
}

func runEnvSet(cmd *cobra.Command, args []string) error {
	targetEnv, _ := cmd.Flags().GetString("environment")

	// Parse KEY=VALUE
	parts := strings.SplitN(args[0], "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid format: expected KEY=VALUE, got %q", args[0])
	}
	key, value := parts[0], parts[1]

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	// Find config file
	loader := config.NewLoader(".")
	configPath, err := loader.FindConfigFile()
	if err != nil {
		return fmt.Errorf("no kbox.yaml found\n  Run 'kbox init' to create one")
	}

	// Load YAML preserving comments
	node, err := config.LoadYAMLWithComments(configPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", configPath, err)
	}

	root := config.GetRootDocument(node)

	var envNode *yaml.Node
	var location string

	if targetEnv == "" {
		// Set in spec.env
		envNode = config.EnsureEnvNode(root)
		location = "spec.env"
	} else {
		// Set in environments.<env>.env
		envNode = config.EnsureEnvironmentEnvNode(root, targetEnv)
		location = fmt.Sprintf("environments.%s.env", targetEnv)
	}

	// Set the value
	config.SetMapKey(envNode, key, value)

	// Save
	if err := config.SaveYAMLWithComments(configPath, node); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}

	fmt.Printf("Updated %s:\n", configPath)
	fmt.Printf("  %s:\n", location)
	fmt.Printf("    + %s = %s\n", key, value)
	fmt.Println()

	if targetEnv == "" {
		fmt.Println("Run 'kbox up' to apply changes.")
	} else {
		fmt.Printf("Run 'kbox deploy -e %s' to apply changes.\n", targetEnv)
	}

	return nil
}

func runEnvUnset(cmd *cobra.Command, args []string) error {
	targetEnv, _ := cmd.Flags().GetString("environment")
	key := args[0]

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	// Find config file
	loader := config.NewLoader(".")
	configPath, err := loader.FindConfigFile()
	if err != nil {
		return fmt.Errorf("no kbox.yaml found\n  Run 'kbox init' to create one")
	}

	// Load YAML preserving comments
	node, err := config.LoadYAMLWithComments(configPath)
	if err != nil {
		return fmt.Errorf("failed to load %s: %w", configPath, err)
	}

	root := config.GetRootDocument(node)
	var location string
	var removed bool

	if targetEnv == "" {
		// Remove from spec.env
		specNode := config.FindMapKey(root, "spec")
		if specNode != nil {
			envNode := config.FindMapKey(specNode, "env")
			if envNode != nil {
				removed = config.RemoveMapKey(envNode, key)
			}
		}
		location = "spec.env"
	} else {
		// Remove from environments.<env>.env
		envsNode := config.FindMapKey(root, "environments")
		if envsNode != nil {
			targetEnvNode := config.FindMapKey(envsNode, targetEnv)
			if targetEnvNode != nil {
				envNode := config.FindMapKey(targetEnvNode, "env")
				if envNode != nil {
					removed = config.RemoveMapKey(envNode, key)
				}
			}
		}
		location = fmt.Sprintf("environments.%s.env", targetEnv)
	}

	if !removed {
		return fmt.Errorf("%s not found in %s", key, location)
	}

	// Save
	if err := config.SaveYAMLWithComments(configPath, node); err != nil {
		return fmt.Errorf("failed to write %s: %w", configPath, err)
	}

	fmt.Printf("Updated %s:\n", configPath)
	fmt.Printf("  %s:\n", location)
	fmt.Printf("    - %s\n", key)
	fmt.Println()

	if targetEnv == "" {
		fmt.Println("Run 'kbox up' to apply changes.")
	} else {
		fmt.Printf("Run 'kbox deploy -e %s' to apply changes.\n", targetEnv)
	}

	return nil
}

func printEnvVars(envVars map[string]string) {
	printEnvVarsIndented(envVars, "  ")
}

func printEnvVarsIndented(envVars map[string]string, indent string) {
	// Sort keys for consistent output
	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, k := range keys {
		fmt.Fprintf(w, "%s%s\t%s\n", indent, k, envVars[k])
	}
	w.Flush()
}
