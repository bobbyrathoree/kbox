package cli

import (
	"fmt"
	"os"

	"github.com/bobbyrathoree/kbox/internal/apply"
	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/render"
	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy an app to Kubernetes",
	Long: `Deploy an application using kbox.yaml configuration.

This command:
  1. Renders Kubernetes manifests from kbox.yaml
  2. Applies them using Server-Side Apply (SSA)
  3. Waits for the deployment to complete

Examples:
  kbox deploy              # Deploy with default environment
  kbox deploy -e prod      # Deploy with prod environment overlay
  kbox deploy --dry-run    # Show what would be deployed`,
	RunE: runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
	env, _ := cmd.Flags().GetString("env")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noWait, _ := cmd.Flags().GetBool("no-wait")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Apply environment overlay
	if env != "" {
		cfg = cfg.ForEnvironment(env)
	}

	// Override namespace if specified
	if namespace != "" {
		cfg.Metadata.Namespace = namespace
	}

	// Check if we have an image
	if cfg.Spec.Image == "" && cfg.Spec.Build == nil {
		return fmt.Errorf("no image specified and no build configuration - use 'kbox up' for build+deploy")
	}

	// If only build config, use a placeholder image
	if cfg.Spec.Image == "" && cfg.Spec.Build != nil {
		cfg.Spec.Image = fmt.Sprintf("%s:latest", cfg.Metadata.Name)
	}

	// Render
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return fmt.Errorf("failed to render: %w", err)
	}

	// Dry run - just show what would be applied
	if dryRun {
		fmt.Println("Dry run - would apply:")
		return bundle.ToYAML(os.Stdout)
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	targetNS := cfg.Metadata.Namespace
	if targetNS == "" {
		targetNS = client.Namespace
	}

	// Print header
	fmt.Printf("Deploying %s to %s (context: %s)\n", cfg.Metadata.Name, targetNS, client.Context)
	if env != "" {
		fmt.Printf("Environment: %s\n", env)
	}
	fmt.Println()

	// Apply
	engine := apply.NewEngine(client.Clientset, os.Stdout)
	result, err := engine.Apply(cmd.Context(), bundle)
	if err != nil {
		return err
	}

	// Check for errors
	if len(result.Errors) > 0 {
		fmt.Fprintln(os.Stderr, "\nErrors:")
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "  - %v\n", e)
		}
		return fmt.Errorf("deploy completed with %d errors", len(result.Errors))
	}

	// Wait for rollout
	if !noWait && bundle.Deployment != nil {
		if err := engine.WaitForRollout(cmd.Context(), targetNS, bundle.Deployment.Name); err != nil {
			return fmt.Errorf("rollout failed: %w", err)
		}
	}

	// Summary
	fmt.Println()
	fmt.Printf("Deploy complete: %d created, %d updated\n",
		len(result.Created), len(result.Updated))

	return nil
}

func init() {
	deployCmd.Flags().StringP("env", "e", "", "Environment overlay to apply")
	deployCmd.Flags().Bool("dry-run", false, "Show what would be deployed without applying")
	deployCmd.Flags().Bool("no-wait", false, "Don't wait for rollout to complete")
	rootCmd.AddCommand(deployCmd)
}
