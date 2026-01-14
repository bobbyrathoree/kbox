package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/bobbyrathoree/kbox/internal/config"
)

func newInitCmd() *cobra.Command {
	var (
		force     bool
		name      string
		port      int
		image     string
		namespace string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new kbox.yaml configuration",
		Long: `Create a new kbox.yaml configuration file for your application.

This command will:
1. Scan for existing Dockerfile and infer settings
2. Look for existing Kubernetes manifests
3. Generate a kbox.yaml with sensible defaults

Use flags to override auto-detected values.`,
		Example: `  # Auto-detect everything
  kbox init

  # Specify app name
  kbox init --name myapp

  # Override port
  kbox init --port 3000

  # Force overwrite existing kbox.yaml
  kbox init --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(initOptions{
				force:     force,
				name:      name,
				port:      port,
				image:     image,
				namespace: namespace,
			})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing kbox.yaml")
	cmd.Flags().StringVar(&name, "name", "", "Application name (default: directory name)")
	cmd.Flags().IntVar(&port, "port", 0, "Application port (default: from Dockerfile EXPOSE)")
	cmd.Flags().StringVar(&image, "image", "", "Docker image (default: app name)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace")

	return cmd
}

type initOptions struct {
	force     bool
	name      string
	port      int
	image     string
	namespace string
}

func runInit(opts initOptions) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Check if kbox.yaml already exists
	configPath := filepath.Join(workDir, config.DefaultConfigFile)
	if !opts.force {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("kbox.yaml already exists (use --force to overwrite)")
		}
	}

	fmt.Println("Scanning directory...")

	// Start with defaults
	cfg := &config.AppConfig{
		APIVersion: config.DefaultAPIVersion,
		Kind:       config.DefaultKind,
		Metadata: config.Metadata{
			Name: opts.name,
		},
		Spec: config.AppSpec{
			Image:    opts.image,
			Port:     opts.port,
			Replicas: config.DefaultReplicas,
		},
	}

	// Try to infer from Dockerfile
	hasDockerfile := false
	dockerfilePath := filepath.Join(workDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err == nil {
		hasDockerfile = true
		fmt.Println("  Found: Dockerfile")

		// Infer config from Dockerfile
		inferred, err := config.InferFromDockerfile(workDir)
		if err == nil {
			// Use inferred values if not overridden
			if cfg.Metadata.Name == "" {
				cfg.Metadata.Name = inferred.Metadata.Name
			}
			if cfg.Spec.Port == 0 {
				cfg.Spec.Port = inferred.Spec.Port
			}
			// Set up build config
			cfg.Spec.Build = &config.BuildConfig{
				Dockerfile: "Dockerfile",
				Context:    ".",
			}
		}
	}

	// Check for existing K8s manifests
	k8sDir := filepath.Join(workDir, "k8s")
	manifestFiles := []string{}
	if entries, err := os.ReadDir(k8sDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && (filepath.Ext(entry.Name()) == ".yaml" || filepath.Ext(entry.Name()) == ".yml") {
				manifestFiles = append(manifestFiles, filepath.Join("k8s", entry.Name()))
				fmt.Printf("  Found: %s\n", filepath.Join("k8s", entry.Name()))
			}
		}
	}

	// Also check root directory for common K8s files
	rootManifests := []string{"deployment.yaml", "deployment.yml", "service.yaml", "service.yml"}
	for _, name := range rootManifests {
		if _, err := os.Stat(filepath.Join(workDir, name)); err == nil {
			manifestFiles = append(manifestFiles, name)
			fmt.Printf("  Found: %s\n", name)
		}
	}

	// If we found K8s manifests, offer to include them
	if len(manifestFiles) > 0 {
		cfg.Spec.Include = manifestFiles
		fmt.Printf("\nNote: Found %d existing manifest(s) - added to 'include' section\n", len(manifestFiles))
	}

	// If name is still empty, use directory name
	if cfg.Metadata.Name == "" {
		cfg.Metadata.Name = filepath.Base(workDir)
	}

	// If namespace specified
	if opts.namespace != "" {
		cfg.Metadata.Namespace = opts.namespace
	}

	// Set image to app name if not specified
	if cfg.Spec.Image == "" {
		cfg.Spec.Image = cfg.Metadata.Name
	}

	// Default port if still not set
	if cfg.Spec.Port == 0 {
		cfg.Spec.Port = config.DefaultPort
	}

	// Generate the YAML
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate YAML: %w", err)
	}

	// Add header comment
	output := fmt.Sprintf(`# kbox configuration
# Docs: https://github.com/bobbyrathoree/kbox
#
# Quick reference:
#   kbox deploy -e dev    Deploy to dev environment
#   kbox up               Build and deploy (zero-config)
#   kbox logs             Stream logs with K8s events
#   kbox rollback         Revert to previous release

%s`, string(yamlBytes))

	// Write file
	if err := os.WriteFile(configPath, []byte(output), 0644); err != nil {
		return fmt.Errorf("failed to write kbox.yaml: %w", err)
	}

	fmt.Println()
	fmt.Printf("Created %s\n", config.DefaultConfigFile)
	fmt.Println()

	// Print summary
	fmt.Println("Configuration:")
	fmt.Printf("  name:     %s\n", cfg.Metadata.Name)
	if cfg.Metadata.Namespace != "" {
		fmt.Printf("  namespace: %s\n", cfg.Metadata.Namespace)
	}
	fmt.Printf("  port:     %d\n", cfg.Spec.Port)
	fmt.Printf("  image:    %s\n", cfg.Spec.Image)
	fmt.Printf("  replicas: %d\n", cfg.Spec.Replicas)

	if hasDockerfile {
		fmt.Println()
		fmt.Println("Build config detected from Dockerfile")
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  kbox render      Preview generated manifests")
	fmt.Println("  kbox diff        Show what would change")
	fmt.Println("  kbox deploy      Deploy to your cluster")

	return nil
}

func init() {
	rootCmd.AddCommand(newInitCmd())
}
