package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/apply"
	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/output"
	"github.com/bobbyrathoree/kbox/internal/release"
	"github.com/bobbyrathoree/kbox/internal/render"
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
	configFile, _ := cmd.Flags().GetString("file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noWait, _ := cmd.Flags().GetBool("no-wait")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// CI mode and output format
	ciMode := IsCIMode(cmd)
	outputFormat := GetOutputFormat(cmd)
	timer := output.NewTimer()

	// Prepare result for CI/JSON output
	result := &output.DeployResult{
		Success: false,
	}

	// Helper to finalize and return
	finalize := func(err error) error {
		result.DurationMs = timer.ElapsedMs()
		if err != nil {
			result.Error = err.Error()
		}

		// JSON output
		if outputFormat == "json" {
			output.NewWriter(os.Stdout, outputFormat, ciMode).WriteDeployResult(result)
			if !result.Success {
				os.Exit(1)
			}
			return nil
		}

		return err
	}

	// Load config
	loader := config.NewLoader(".")

	// If specific file provided, load directly (skip multi-service detection)
	if configFile != "" {
		return deployFromFile(cmd, configFile, env, namespace, kubeContext, dryRun, noWait, timeout, ciMode, outputFormat, timer)
	}

	// Check if this is a multi-service config
	isMulti, err := loader.IsMultiService()
	if err != nil {
		return finalize(fmt.Errorf("failed to load kbox.yaml: %w\n  → Run 'kbox init' to create one, or use 'kbox up' for zero-config deploy", err))
	}

	var bundle *render.Bundle
	var appName string
	var targetNamespace string

	if isMulti {
		// Handle multi-service config
		multiCfg, err := loader.LoadMultiService()
		if err != nil {
			return finalize(fmt.Errorf("failed to load kbox.yaml: %w", err))
		}

		appName = multiCfg.Metadata.Name
		result.App = appName

		// Apply environment overlay
		if env != "" {
			multiCfg = multiCfg.ForEnvironment(env)
		}

		// Override namespace if specified
		if namespace != "" {
			multiCfg.Metadata.Namespace = namespace
		}
		targetNamespace = multiCfg.Metadata.Namespace

		// Render using multi-service renderer
		renderer := render.NewMultiService(multiCfg)
		bundle, err = renderer.Render()
		if err != nil {
			return finalize(fmt.Errorf("failed to render: %w", err))
		}
	} else {
		// Handle single-service config
		cfg, err := loader.Load()
		if err != nil {
			return finalize(fmt.Errorf("failed to load kbox.yaml: %w\n  → Run 'kbox init' to create one, or use 'kbox up' for zero-config deploy", err))
		}

		// Validate with warnings for security issues
		warnings, err := config.ValidateWithWarnings(cfg)
		if err != nil {
			return finalize(fmt.Errorf("validation failed: %w", err))
		}
		// Print warnings to stderr (unless JSON output or CI mode suppresses them)
		if !ciMode && outputFormat != "json" {
			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
			}
		}

		appName = cfg.Metadata.Name
		result.App = appName

		// Apply environment overlay
		if env != "" {
			cfg = cfg.ForEnvironment(env)
		}

		// Override namespace if specified
		if namespace != "" {
			cfg.Metadata.Namespace = namespace
		}
		targetNamespace = cfg.Metadata.Namespace

		// Check if we have an image
		if cfg.Spec.Image == "" && cfg.Spec.Build == nil {
			return finalize(fmt.Errorf("no image specified in kbox.yaml\n\n" +
				"Choose one:\n" +
				"  kbox up      → Build from Dockerfile + deploy (for development)\n" +
				"  kbox deploy  → Deploy pre-built image (add 'image:' to kbox.yaml)"))
		}

		// If only build config, use a placeholder image
		if cfg.Spec.Image == "" && cfg.Spec.Build != nil {
			cfg.Spec.Image = fmt.Sprintf("%s:latest", cfg.Metadata.Name)
		}

		// Render
		renderer := render.New(cfg)
		bundle, err = renderer.Render()
		if err != nil {
			return finalize(fmt.Errorf("failed to render: %w", err))
		}
	}

	// Dry run - show what would be applied
	if dryRun {
		if outputFormat == "json" {
			// JSON output for CI pipelines
			return bundle.ToJSON(os.Stdout)
		}
		// Show resource summary
		fmt.Println("Dry run - would deploy the following resources:")
		fmt.Println()
		printDeployPreview(bundle)
		fmt.Println()
		fmt.Println("Run without --dry-run to apply these resources.")
		return nil
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return finalize(fmt.Errorf("failed to connect to cluster: %w\n  → Run 'kbox doctor' to diagnose connection issues", err))
	}

	result.Context = client.Context

	targetNS := targetNamespace
	if targetNS == "" {
		targetNS = client.Namespace
	}
	result.Namespace = targetNS

	// Print header (unless CI mode with JSON output)
	if !ciMode || outputFormat != "json" {
		fmt.Printf("Deploying %s to %s (context: %s)\n", appName, targetNS, client.Context)
		if env != "" {
			fmt.Printf("Environment: %s\n", env)
		}
		fmt.Println()
	}

	// Determine output writer for apply engine
	var applyOut io.Writer = os.Stdout
	if ciMode {
		applyOut = io.Discard // Suppress apply output in CI mode
	}

	// Apply
	engine := apply.NewEngine(client.Clientset, applyOut)
	if timeout > 0 {
		engine.SetTimeout(timeout)
	}
	applyResult, err := engine.Apply(cmd.Context(), bundle)
	if err != nil {
		return finalize(err)
	}

	// Build resource results
	for _, name := range applyResult.Created {
		result.Resources = append(result.Resources, output.ResourceResult{
			Kind:   extractKind(name),
			Name:   extractName(name),
			Action: "created",
		})
	}
	for _, name := range applyResult.Updated {
		result.Resources = append(result.Resources, output.ResourceResult{
			Kind:   extractKind(name),
			Name:   extractName(name),
			Action: "updated",
		})
	}

	// Check for errors
	if len(applyResult.Errors) > 0 {
		if !ciMode {
			fmt.Fprintln(os.Stderr, "\nErrors:")
			for _, e := range applyResult.Errors {
				fmt.Fprintf(os.Stderr, "  - %v\n", e)
			}
		}
		return finalize(fmt.Errorf("deploy completed with %d errors", len(applyResult.Errors)))
	}

	// Wait for rollout
	if !noWait && bundle.Deployment != nil {
		if err := engine.WaitForRollout(cmd.Context(), targetNS, bundle.Deployment.Name); err != nil {
			return finalize(fmt.Errorf("rollout failed: %w\n  → Run 'kbox logs' to see pod logs\n  → Run 'kbox status' to check deployment state", err))
		}
	}

	// Save release to history (single-service only for now)
	if !isMulti {
		cfg, _ := loader.Load()
		store := release.NewStore(client.Clientset, targetNS, appName)
		revision, err := store.Save(cmd.Context(), cfg)
		if err != nil {
			// Non-fatal - deployment succeeded
			if !ciMode {
				fmt.Fprintf(os.Stderr, "Warning: failed to save release history: %v\n", err)
			}
		}
		result.Revision = revision
	}

	// Mark success
	result.Success = true

	// Summary (unless JSON mode)
	if outputFormat != "json" {
		fmt.Println()
		fmt.Printf("Deploy complete: %d created, %d updated\n",
			len(applyResult.Created), len(applyResult.Updated))
		if result.Revision > 0 {
			fmt.Printf("Release %s saved (rollback available)\n", release.FormatRevision(result.Revision))
		}
	}

	return finalize(nil)
}

// extractKind extracts the kind from "Kind/Name" format
func extractKind(s string) string {
	for i, c := range s {
		if c == '/' {
			return s[:i]
		}
	}
	return s
}

// extractName extracts the name from "Kind/Name" format
func extractName(s string) string {
	for i, c := range s {
		if c == '/' {
			return s[i+1:]
		}
	}
	return s
}

// printDeployPreview prints a preview of resources that would be deployed
func printDeployPreview(bundle *render.Bundle) {
	total := len(bundle.AllObjects())
	fmt.Printf("  Total resources: %d\n\n", total)

	// Core workload
	if bundle.Deployment != nil {
		replicas := int32(1)
		if bundle.Deployment.Spec.Replicas != nil {
			replicas = *bundle.Deployment.Spec.Replicas
		}
		fmt.Printf("  Deployment:      %s (%d replicas)\n", bundle.Deployment.Name, replicas)
		if len(bundle.Deployment.Spec.Template.Spec.Containers) > 0 {
			fmt.Printf("    Image: %s\n", bundle.Deployment.Spec.Template.Spec.Containers[0].Image)
		}
	}

	// Services
	if len(bundle.Services) > 0 {
		fmt.Printf("  Services:        %d\n", len(bundle.Services))
		for _, svc := range bundle.Services {
			svcType := svc.Spec.Type
			if svcType == "" {
				svcType = "ClusterIP"
			}
			fmt.Printf("    - %s (%s)\n", svc.Name, svcType)
		}
	}

	// Secrets
	if len(bundle.Secrets) > 0 {
		fmt.Printf("  Secrets:         %d\n", len(bundle.Secrets))
	}

	// Service account
	if bundle.ServiceAccount != nil {
		fmt.Printf("  ServiceAccount:  %s\n", bundle.ServiceAccount.Name)
	}

	// Autoscaling
	if bundle.HPA != nil {
		fmt.Printf("  HPA:             %s (min: %d, max: %d)\n",
			bundle.HPA.Name,
			*bundle.HPA.Spec.MinReplicas,
			bundle.HPA.Spec.MaxReplicas)
	}

	// PDB
	if bundle.PDB != nil {
		fmt.Printf("  PDB:             %s\n", bundle.PDB.Name)
	}

	// Network policies
	if len(bundle.NetworkPolicies) > 0 {
		fmt.Printf("  NetworkPolicies: %d\n", len(bundle.NetworkPolicies))
	}

	// Dependencies
	if len(bundle.StatefulSets) > 0 {
		fmt.Printf("\n  Dependencies:\n")
		for _, ss := range bundle.StatefulSets {
			var storage string
			if len(ss.Spec.VolumeClaimTemplates) > 0 {
				storage = ss.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String()
			}
			fmt.Printf("    - %s (StatefulSet, %s storage)\n", ss.Name, storage)
		}
	}
}

// deployFromFile handles deployment from a specific config file
func deployFromFile(cmd *cobra.Command, configFile, env, namespace, kubeContext string, dryRun, noWait bool, timeout time.Duration, ciMode bool, outputFormat string, timer *output.Timer) error {
	result := &output.DeployResult{Success: false}

	finalize := func(err error) error {
		result.DurationMs = timer.ElapsedMs()
		if err != nil {
			result.Error = err.Error()
		}
		if outputFormat == "json" {
			output.NewWriter(os.Stdout, outputFormat, ciMode).WriteDeployResult(result)
			if !result.Success {
				os.Exit(1)
			}
			return nil
		}
		return err
	}

	loader := config.NewLoader(".")
	cfg, err := loader.LoadFile(configFile)
	if err != nil {
		return finalize(fmt.Errorf("failed to load %s: %w", configFile, err))
	}

	// Validate with warnings
	warnings, err := config.ValidateWithWarnings(cfg)
	if err != nil {
		return finalize(fmt.Errorf("validation failed: %w", err))
	}
	if !ciMode && outputFormat != "json" {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
		}
	}

	appName := cfg.Metadata.Name
	result.App = appName

	// Apply environment overlay
	if env != "" {
		cfg = cfg.ForEnvironment(env)
	}

	// Override namespace if specified
	if namespace != "" {
		cfg.Metadata.Namespace = namespace
	}
	targetNamespace := cfg.Metadata.Namespace

	// Check if we have an image
	if cfg.Spec.Image == "" && cfg.Spec.Build == nil {
		return finalize(fmt.Errorf("no image specified in %s\n\n"+
			"Choose one:\n"+
			"  kbox up      → Build from Dockerfile + deploy (for development)\n"+
			"  kbox deploy  → Deploy pre-built image (add 'image:' to kbox.yaml)", configFile))
	}

	// If only build config, use a placeholder image
	if cfg.Spec.Image == "" && cfg.Spec.Build != nil {
		cfg.Spec.Image = fmt.Sprintf("%s:latest", cfg.Metadata.Name)
	}

	// Render
	renderer := render.New(cfg)
	bundle, err := renderer.Render()
	if err != nil {
		return finalize(fmt.Errorf("failed to render: %w", err))
	}

	// Dry run - show what would be applied
	if dryRun {
		if outputFormat == "json" {
			return bundle.ToJSON(os.Stdout)
		}
		fmt.Println("Dry run - would deploy the following resources:")
		fmt.Println()
		printDeployPreview(bundle)
		fmt.Println()
		fmt.Println("Run without --dry-run to apply these resources.")
		return nil
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return finalize(fmt.Errorf("failed to connect to cluster: %w\n  → Run 'kbox doctor' to diagnose connection issues", err))
	}

	result.Context = client.Context
	targetNS := targetNamespace
	if targetNS == "" {
		targetNS = client.Namespace
	}
	result.Namespace = targetNS

	// Print header
	if !ciMode || outputFormat != "json" {
		fmt.Printf("Deploying %s to %s (context: %s)\n", appName, targetNS, client.Context)
		if env != "" {
			fmt.Printf("Environment: %s\n", env)
		}
		fmt.Println()
	}

	// Determine output writer for apply engine
	var applyOut io.Writer = os.Stdout
	if ciMode {
		applyOut = io.Discard
	}

	// Apply
	engine := apply.NewEngine(client.Clientset, applyOut)
	if timeout > 0 {
		engine.SetTimeout(timeout)
	}
	applyResult, err := engine.Apply(cmd.Context(), bundle)
	if err != nil {
		return finalize(err)
	}

	// Build resource results
	for _, name := range applyResult.Created {
		result.Resources = append(result.Resources, output.ResourceResult{
			Kind:   extractKind(name),
			Name:   extractName(name),
			Action: "created",
		})
	}
	for _, name := range applyResult.Updated {
		result.Resources = append(result.Resources, output.ResourceResult{
			Kind:   extractKind(name),
			Name:   extractName(name),
			Action: "updated",
		})
	}

	// Check for errors
	if len(applyResult.Errors) > 0 {
		if !ciMode {
			fmt.Fprintln(os.Stderr, "\nErrors:")
			for _, e := range applyResult.Errors {
				fmt.Fprintf(os.Stderr, "  - %v\n", e)
			}
		}
		return finalize(fmt.Errorf("deploy completed with %d errors", len(applyResult.Errors)))
	}

	// Wait for rollout
	if !noWait && bundle.Deployment != nil {
		if err := engine.WaitForRollout(cmd.Context(), targetNS, bundle.Deployment.Name); err != nil {
			return finalize(fmt.Errorf("rollout failed: %w\n  → Run 'kbox logs' to see pod logs\n  → Run 'kbox status' to check deployment state", err))
		}
	}

	// Save release to history
	store := release.NewStore(client.Clientset, targetNS, appName)
	revision, err := store.Save(cmd.Context(), cfg)
	if err != nil {
		if !ciMode {
			fmt.Fprintf(os.Stderr, "Warning: failed to save release history: %v\n", err)
		}
	}
	result.Revision = revision

	// Mark success
	result.Success = true

	// Summary
	if outputFormat != "json" {
		fmt.Println()
		fmt.Printf("Deploy complete: %d created, %d updated\n",
			len(applyResult.Created), len(applyResult.Updated))
		if result.Revision > 0 {
			fmt.Printf("Release %s saved (rollback available)\n", release.FormatRevision(result.Revision))
		}
	}

	return finalize(nil)
}

func init() {
	deployCmd.Flags().StringP("env", "e", "", "Environment overlay to apply")
	deployCmd.Flags().StringP("file", "f", "", "Path to kbox.yaml (default: ./kbox.yaml)")
	deployCmd.Flags().Bool("dry-run", false, "Show what would be deployed without applying")
	deployCmd.Flags().Bool("no-wait", false, "Don't wait for rollout to complete")
	deployCmd.Flags().Duration("timeout", 5*time.Minute, "Timeout for rollout completion (e.g., 10m, 30s)")
	rootCmd.AddCommand(deployCmd)
}
