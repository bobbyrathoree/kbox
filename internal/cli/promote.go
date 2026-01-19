package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/apply"
	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/release"
	"github.com/bobbyrathoree/kbox/internal/render"
)

var promoteCmd = &cobra.Command{
	Use:   "promote <environment>",
	Short: "Promote release to environment",
	Long: `Promote the current release to a target environment.

Takes the latest release from the current namespace and deploys it to the
target environment's namespace, applying any environment-specific overrides.

Requires environments to be defined in kbox.yaml:

  environments:
    staging:
      namespace: myapp-staging
      replicas: 2
    prod:
      namespace: myapp-prod
      context: prod-cluster
      replicas: 5

Examples:
  kbox promote staging           # Promote to staging
  kbox promote prod              # Promote to prod
  kbox promote prod --revision 5 # Promote specific revision
  kbox promote prod --yes        # Skip confirmation
  kbox promote prod --dry-run    # Preview without deploying`,
	Args: cobra.ExactArgs(1),
	RunE: runPromote,
}

func init() {
	promoteCmd.Flags().IntP("revision", "r", 0, "Source revision to promote (default: latest)")
	promoteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	promoteCmd.Flags().Bool("dry-run", false, "Show what would change without deploying")

	rootCmd.AddCommand(promoteCmd)
}

func runPromote(cmd *cobra.Command, args []string) error {
	targetEnv := args[0]
	revision, _ := cmd.Flags().GetInt("revision")
	skipConfirm, _ := cmd.Flags().GetBool("yes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	sourceNS, _ := cmd.Flags().GetString("namespace")
	sourceCtx, _ := cmd.Flags().GetString("context")
	ciMode, _ := cmd.Flags().GetBool("ci")

	// Load kbox.yaml
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Verify target environment exists
	envOverride, ok := cfg.Environments[targetEnv]
	if !ok {
		return fmt.Errorf("environment %q not defined in kbox.yaml\n  Add it with:\n    environments:\n      %s:\n        namespace: %s-%s", targetEnv, targetEnv, cfg.Metadata.Name, targetEnv)
	}

	// Get target namespace/context
	targetNS := envOverride.Namespace
	if targetNS == "" {
		return fmt.Errorf("environment %q has no namespace defined\n  Add 'namespace' to environments.%s in kbox.yaml", targetEnv, targetEnv)
	}
	targetCtx := envOverride.Context

	// Connect to source cluster
	sourceClient, err := k8s.NewClient(k8s.ClientOptions{
		Context:   sourceCtx,
		Namespace: sourceNS,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to source cluster: %w", err)
	}

	// Determine source namespace
	srcNS := sourceNS
	if srcNS == "" {
		srcNS = cfg.Metadata.Namespace
	}
	if srcNS == "" {
		srcNS = sourceClient.Namespace
	}

	// Get source release
	sourceStore := release.NewStore(sourceClient.Clientset, srcNS, cfg.Metadata.Name)
	var sourceRelease *release.Release
	if revision > 0 {
		sourceRelease, err = sourceStore.Get(cmd.Context(), revision)
	} else {
		sourceRelease, err = sourceStore.GetLatest(cmd.Context())
	}
	if err != nil {
		return fmt.Errorf("failed to get source release: %w\n  Deploy first with: kbox up", err)
	}

	// Deserialize source config
	var sourceCfg config.AppConfig
	if err := json.Unmarshal([]byte(sourceRelease.Config), &sourceCfg); err != nil {
		return fmt.Errorf("failed to deserialize source config: %w", err)
	}

	// Apply target environment overlay
	targetCfg := cfg.ForEnvironment(targetEnv)
	targetCfg.Metadata.Namespace = targetNS

	// Use source image if not overridden
	if targetCfg.Spec.Image == "" || targetCfg.Spec.Image == cfg.Spec.Image {
		targetCfg.Spec.Image = sourceCfg.Spec.Image
	}

	// Print promotion info
	fmt.Fprintf(os.Stderr, "Promoting %s to %s...\n\n", cfg.Metadata.Name, targetEnv)

	fmt.Println("Source:")
	fmt.Printf("  Namespace: %s\n", srcNS)
	fmt.Printf("  Revision:  %s\n", release.FormatRevision(sourceRelease.Revision))
	fmt.Printf("  Image:     %s\n", sourceRelease.Image)
	fmt.Println()

	fmt.Println("Target:")
	fmt.Printf("  Namespace: %s\n", targetNS)
	if targetCtx != "" {
		fmt.Printf("  Context:   %s\n", targetCtx)
	}
	fmt.Println()

	// Show changes
	fmt.Println("Configuration:")
	printConfigDiff(&sourceCfg, targetCfg)
	fmt.Println()

	if dryRun {
		fmt.Println("Dry run - no changes made")
		return nil
	}

	// Confirm
	if !skipConfirm && !ciMode {
		fmt.Print("Proceed? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cancelled")
			return nil
		}
		fmt.Println()
	}

	// Connect to target cluster (may be different context)
	targetClient, err := k8s.NewClient(k8s.ClientOptions{
		Context:   targetCtx,
		Namespace: targetNS,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to target cluster: %w", err)
	}

	// Render manifests
	renderer := render.New(targetCfg)
	bundle, err := renderer.Render()
	if err != nil {
		return fmt.Errorf("failed to render manifests: %w", err)
	}

	// Apply to target
	fmt.Fprintf(os.Stderr, "Deploying to %s...\n", targetNS)
	engine := apply.NewEngine(targetClient.Clientset, os.Stdout)
	_, err = engine.Apply(context.Background(), bundle)
	if err != nil {
		return fmt.Errorf("failed to apply to target: %w", err)
	}

	// Save release in target namespace
	targetStore := release.NewStore(targetClient.Clientset, targetNS, cfg.Metadata.Name)
	targetRevision, err := targetStore.Save(cmd.Context(), targetCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save release history: %v\n", err)
	}

	fmt.Println()
	fmt.Printf("Promoted successfully!\n")
	fmt.Printf("  Release: %s in %s\n", release.FormatRevision(targetRevision), targetNS)
	fmt.Println()
	fmt.Printf("View status: kbox status -n %s\n", targetNS)

	return nil
}

func printConfigDiff(source, target *config.AppConfig) {
	// Compare replicas
	if source.Spec.Replicas != target.Spec.Replicas {
		fmt.Printf("  replicas: %d → %d\n", source.Spec.Replicas, target.Spec.Replicas)
	} else {
		fmt.Printf("  replicas: %d\n", target.Spec.Replicas)
	}

	// Compare image
	if source.Spec.Image != target.Spec.Image {
		fmt.Printf("  image: %s → %s\n", source.Spec.Image, target.Spec.Image)
	} else {
		fmt.Printf("  image: %s\n", target.Spec.Image)
	}

	// Compare resources
	if target.Spec.Resources != nil {
		if source.Spec.Resources == nil {
			fmt.Printf("  resources.memory: (none) → %s\n", target.Spec.Resources.Memory)
			fmt.Printf("  resources.cpu: (none) → %s\n", target.Spec.Resources.CPU)
		} else {
			if source.Spec.Resources.Memory != target.Spec.Resources.Memory {
				fmt.Printf("  resources.memory: %s → %s\n", source.Spec.Resources.Memory, target.Spec.Resources.Memory)
			}
			if source.Spec.Resources.CPU != target.Spec.Resources.CPU {
				fmt.Printf("  resources.cpu: %s → %s\n", source.Spec.Resources.CPU, target.Spec.Resources.CPU)
			}
		}
	}

	// Compare env vars (show only changes)
	for k, v := range target.Spec.Env {
		if source.Spec.Env[k] != v {
			if source.Spec.Env[k] == "" {
				fmt.Printf("  env.%s: (new) %s\n", k, v)
			} else {
				fmt.Printf("  env.%s: %s → %s\n", k, source.Spec.Env[k], v)
			}
		}
	}
}
