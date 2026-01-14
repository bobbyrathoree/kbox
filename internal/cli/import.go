package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/bobbyrathoree/kbox/internal/importer"
)

func newImportCmd() *cobra.Command {
	var (
		directory  string
		outputFile string
	)

	cmd := &cobra.Command{
		Use:   "import [files...]",
		Short: "Import existing Kubernetes YAML to kbox.yaml",
		Long: `Convert existing Kubernetes manifests to kbox.yaml format.

Reads Deployment, Service, ConfigMap, and Secret resources from YAML files
and generates an equivalent kbox.yaml configuration.

Supported resources:
  - Deployment (required - at least one must be present)
  - Service
  - ConfigMap
  - Secret

The import process extracts:
  - App name from Deployment metadata
  - Image, port, replicas from container spec
  - Environment variables (direct values only)
  - Resource requests/limits
  - Health check paths from probes
  - Service type (if not ClusterIP)`,
		Example: `  # Import from specific files
  kbox import deployment.yaml service.yaml

  # Import from a directory
  kbox import -f manifests/

  # Output to a specific file
  kbox import -o kbox.yaml deployment.yaml

  # Import and preview (stdout)
  kbox import deployment.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, args, directory, outputFile)
		},
	}

	cmd.Flags().StringVarP(&directory, "dir", "f", "", "Directory containing YAML files to import")
	cmd.Flags().StringVarP(&outputFile, "output", "o", "", "Output file (default: stdout)")

	return cmd
}

func runImport(cmd *cobra.Command, args []string, directory, outputFile string) error {
	ciMode := IsCIMode(cmd)

	// Collect input files
	var files []string

	// Add files from directory
	if directory != "" {
		info, err := os.Stat(directory)
		if err != nil {
			return fmt.Errorf("cannot access %s: %w", directory, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", directory)
		}

		entries, err := os.ReadDir(directory)
		if err != nil {
			return fmt.Errorf("failed to read directory: %w", err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if hasYAMLExtension(name) {
				files = append(files, filepath.Join(directory, name))
			}
		}
	}

	// Add files from positional arguments
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return fmt.Errorf("cannot access %s: %w", arg, err)
		}

		if info.IsDir() {
			// If arg is a directory, add all YAML files from it
			entries, err := os.ReadDir(arg)
			if err != nil {
				return fmt.Errorf("failed to read directory %s: %w", arg, err)
			}
			for _, entry := range entries {
				if !entry.IsDir() && hasYAMLExtension(entry.Name()) {
					files = append(files, filepath.Join(arg, entry.Name()))
				}
			}
		} else {
			files = append(files, arg)
		}
	}

	if len(files) == 0 {
		return fmt.Errorf("no input files specified\n  → Use 'kbox import <files...>' or 'kbox import -f <directory>'")
	}

	// Parse files
	if !ciMode {
		fmt.Fprintf(os.Stderr, "Parsing %d file(s)...\n", len(files))
	}

	resources, err := importer.ParseFiles(files)
	if err != nil {
		return fmt.Errorf("failed to parse files: %w", err)
	}

	if !ciMode {
		fmt.Fprintf(os.Stderr, "Found: %s\n", resources.Summary())
	}

	// Convert to kbox config
	cfg, err := importer.Convert(resources)
	if err != nil {
		return err
	}

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to generate YAML: %w", err)
	}

	// Output
	if outputFile == "" || outputFile == "-" {
		// Write to stdout
		if !ciMode {
			fmt.Fprintln(os.Stderr)
		}
		fmt.Print(string(yamlBytes))
	} else {
		// Write to file
		if err := os.WriteFile(outputFile, yamlBytes, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", outputFile, err)
		}
		if !ciMode {
			fmt.Fprintf(os.Stderr, "\nCreated %s\n", outputFile)
			fmt.Fprintf(os.Stderr, "  → Run 'kbox deploy' to deploy\n")
		}
	}

	return nil
}

func hasYAMLExtension(name string) bool {
	return len(name) > 5 && (name[len(name)-5:] == ".yaml" || name[len(name)-4:] == ".yml")
}

func init() {
	rootCmd.AddCommand(newImportCmd())
}
