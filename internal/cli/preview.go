package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/apply"
	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/preview"
	"github.com/bobbyrathoree/kbox/internal/render"
)

var previewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Manage preview environments",
	Long: `Create, list, and destroy ephemeral preview environments.

Preview environments are isolated namespaces that contain a full copy
of your application stack. They're perfect for testing pull requests
or feature branches before merging.

Examples:
  kbox preview create --name=pr-123     # Create preview for PR #123
  kbox preview list                     # List all active previews
  kbox preview destroy --name=pr-123    # Clean up preview`,
}

var previewCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a preview environment",
	Long: `Create a new preview environment with an isolated namespace.

The preview will have its own namespace containing all resources
from your kbox.yaml configuration.`,
	Example: `  # Create a preview for pull request #123
  kbox preview create --name=pr-123

  # Create a preview with a custom name
  kbox preview create --name=feature-dark-mode`,
	RunE: runPreviewCreate,
}

var previewDestroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy a preview environment",
	Long: `Delete a preview environment and all its resources.

This deletes the namespace and cascades to all resources within it.`,
	Example: `  # Destroy the preview for PR #123
  kbox preview destroy --name=pr-123`,
	RunE: runPreviewDestroy,
}

var previewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List preview environments",
	Long:  `List all active preview environments for the current app.`,
	Example: `  kbox preview list
  kbox preview list --output=json`,
	RunE: runPreviewList,
}

func runPreviewCreate(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	kubeContext, _ := cmd.Flags().GetString("context")
	ciMode := IsCIMode(cmd)
	outputFormat := GetOutputFormat(cmd)

	if name == "" {
		return fmt.Errorf("preview name is required\n  → Use: kbox preview create --name=<name>")
	}

	// Validate name (same rules as K8s names)
	if !config.IsValidName(name) {
		return fmt.Errorf("invalid preview name %q\n  → Use lowercase letters, numbers, and hyphens only", name)
	}

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context: kubeContext,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Create preview namespace
	mgr := preview.NewManager(client.Clientset, cfg.Metadata.Name)
	info, err := mgr.Create(cmd.Context(), name)
	if err != nil {
		return err
	}

	if !ciMode {
		fmt.Printf("Created preview namespace: %s\n", info.Namespace)
		fmt.Println()
	}

	// Now deploy the app to the preview namespace
	// Override namespace in config
	cfg.Metadata.Namespace = info.Namespace

	// Render
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		// Try to clean up namespace on failure
		_ = mgr.Destroy(cmd.Context(), name)
		return fmt.Errorf("failed to render: %w", err)
	}

	// Apply
	engine := apply.NewEngine(client.Clientset, os.Stdout)
	if ciMode && outputFormat == "json" {
		engine = apply.NewEngine(client.Clientset, nil)
	}

	_, err = engine.Apply(cmd.Context(), bundle)
	if err != nil {
		// Try to clean up namespace on failure
		_ = mgr.Destroy(cmd.Context(), name)
		return fmt.Errorf("failed to deploy to preview: %w", err)
	}

	// Output
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(info)
	}

	if !ciMode {
		fmt.Println()
		fmt.Printf("Preview %q created successfully\n", name)
		fmt.Printf("  Namespace: %s\n", info.Namespace)
		fmt.Println()
		fmt.Printf("  → Run 'kbox deploy -n %s' to update\n", info.Namespace)
		fmt.Printf("  → Run 'kbox preview destroy --name=%s' when done\n", name)
	}

	return nil
}

func runPreviewDestroy(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	kubeContext, _ := cmd.Flags().GetString("context")
	ciMode := IsCIMode(cmd)
	outputFormat := GetOutputFormat(cmd)

	if name == "" {
		return fmt.Errorf("preview name is required\n  → Use: kbox preview destroy --name=<name>")
	}

	// Load config to get app name
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context: kubeContext,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Destroy preview
	mgr := preview.NewManager(client.Clientset, cfg.Metadata.Name)
	err = mgr.Destroy(cmd.Context(), name)
	if err != nil {
		return err
	}

	// Output
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"success": true,
			"name":    name,
			"message": "Preview destroyed",
		})
	}

	if !ciMode {
		fmt.Printf("Preview %q destroyed\n", name)
	}

	return nil
}

func runPreviewList(cmd *cobra.Command, args []string) error {
	kubeContext, _ := cmd.Flags().GetString("context")
	ciMode := IsCIMode(cmd)
	outputFormat := GetOutputFormat(cmd)

	// Load config to get app name
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context: kubeContext,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// List previews
	mgr := preview.NewManager(client.Clientset, cfg.Metadata.Name)
	previews, err := mgr.List(cmd.Context())
	if err != nil {
		return err
	}

	// Output
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(previews)
	}

	if len(previews) == 0 {
		if !ciMode {
			fmt.Println("No active previews")
			fmt.Println("  → Run 'kbox preview create --name=<name>' to create one")
		}
		return nil
	}

	// Table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tNAMESPACE\tAGE\tSTATUS")
	for _, p := range previews {
		age := formatAge(p.Created)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, p.Namespace, age, p.Status)
	}
	w.Flush()

	return nil
}

// formatAge formats a duration since a time as a human-readable age
func formatAge(t time.Time) string {
	d := time.Since(t)

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func init() {
	// Preview create flags
	previewCreateCmd.Flags().String("name", "", "Name for the preview environment (required)")
	previewCreateCmd.MarkFlagRequired("name")

	// Preview destroy flags
	previewDestroyCmd.Flags().String("name", "", "Name of the preview to destroy (required)")
	previewDestroyCmd.MarkFlagRequired("name")

	// Add subcommands
	previewCmd.AddCommand(previewCreateCmd)
	previewCmd.AddCommand(previewDestroyCmd)
	previewCmd.AddCommand(previewListCmd)

	rootCmd.AddCommand(previewCmd)
}
