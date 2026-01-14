package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/render"
)

func newDiffCmd() *cobra.Command {
	var (
		environment string
		configFile  string
	)

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Show what would change on deploy",
		Long: `Preview the changes that would be applied on deploy.

Shows which resources would be created, updated, or unchanged.
Use this before 'kbox deploy' to verify what will happen.`,
		Example: `  # Show diff for default environment
  kbox diff

  # Show diff for production
  kbox diff -e prod

  # Use a specific config file
  kbox diff -f myapp.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load config
			loader := config.NewLoader(".")
			var cfg *config.AppConfig
			var err error
			if configFile != "" {
				cfg, err = loader.LoadFile(configFile)
			} else {
				cfg, err = loader.Load()
			}
			if err != nil {
				return fmt.Errorf("failed to load config: %w\n  → Run 'kbox init' to create a kbox.yaml", err)
			}

			// Apply environment overlay
			if environment != "" {
				cfg = cfg.ForEnvironment(environment)
			}

			// Apply defaults
			cfg.WithDefaults()

			// Validate
			if err := config.Validate(cfg); err != nil {
				return fmt.Errorf("config validation failed: %w", err)
			}

			// Determine namespace
			namespace := cfg.Metadata.Namespace
			if namespace == "" {
				namespace = "default"
			}

			// Get K8s client
			client, err := k8s.NewClient(k8s.ClientOptions{})
			if err != nil {
				return fmt.Errorf("failed to connect to cluster: %w\n  → Run 'kbox doctor' to diagnose connection issues", err)
			}

			// Render the bundle
			renderer := render.New(cfg)
			bundle, err := renderer.Render()
			if err != nil {
				return fmt.Errorf("failed to render: %w", err)
			}

			// Print header
			if environment != "" {
				fmt.Printf("Changes for environment: %s\n", environment)
			} else {
				fmt.Printf("Changes for %s (namespace: %s)\n", cfg.Metadata.Name, namespace)
			}
			fmt.Println()

			// Check each resource
			changes := []changeInfo{}

			// Check ConfigMaps
			for _, cm := range bundle.ConfigMaps {
				existing, err := client.Clientset.CoreV1().ConfigMaps(namespace).Get(ctx, cm.Name, metav1.GetOptions{})
				if err != nil {
					changes = append(changes, changeInfo{
						Kind:   "ConfigMap",
						Name:   cm.Name,
						Action: "create",
					})
				} else {
					// Compare
					if configMapChanged(existing.Data, cm.Data) {
						changes = append(changes, changeInfo{
							Kind:   "ConfigMap",
							Name:   cm.Name,
							Action: "update",
							Detail: "data changed",
						})
					} else {
						changes = append(changes, changeInfo{
							Kind:   "ConfigMap",
							Name:   cm.Name,
							Action: "unchanged",
						})
					}
				}
			}

			// Check Services
			for _, svc := range bundle.Services {
				existing, err := client.Clientset.CoreV1().Services(namespace).Get(ctx, svc.Name, metav1.GetOptions{})
				if err != nil {
					changes = append(changes, changeInfo{
						Kind:   "Service",
						Name:   svc.Name,
						Action: "create",
					})
				} else {
					// Compare ports
					if len(existing.Spec.Ports) != len(svc.Spec.Ports) ||
						(len(svc.Spec.Ports) > 0 && existing.Spec.Ports[0].Port != svc.Spec.Ports[0].Port) {
						changes = append(changes, changeInfo{
							Kind:   "Service",
							Name:   svc.Name,
							Action: "update",
							Detail: fmt.Sprintf("port: %d -> %d", existing.Spec.Ports[0].Port, svc.Spec.Ports[0].Port),
						})
					} else {
						changes = append(changes, changeInfo{
							Kind:   "Service",
							Name:   svc.Name,
							Action: "unchanged",
						})
					}
				}
			}

			// Check Deployment
			if bundle.Deployment != nil {
				dep := bundle.Deployment
				existing, err := client.Clientset.AppsV1().Deployments(namespace).Get(ctx, dep.Name, metav1.GetOptions{})
				if err != nil {
					changes = append(changes, changeInfo{
						Kind:   "Deployment",
						Name:   dep.Name,
						Action: "create",
						Detail: fmt.Sprintf("image: %s, replicas: %d", cfg.Spec.Image, cfg.Spec.Replicas),
					})
				} else {
					// Compare key fields
					details := []string{}

					// Image
					if len(existing.Spec.Template.Spec.Containers) > 0 {
						oldImage := existing.Spec.Template.Spec.Containers[0].Image
						newImage := dep.Spec.Template.Spec.Containers[0].Image
						if oldImage != newImage {
							details = append(details, fmt.Sprintf("image: %s -> %s", truncateImage(oldImage, 30), truncateImage(newImage, 30)))
						}
					}

					// Replicas
					if existing.Spec.Replicas != nil && dep.Spec.Replicas != nil {
						if *existing.Spec.Replicas != *dep.Spec.Replicas {
							details = append(details, fmt.Sprintf("replicas: %d -> %d", *existing.Spec.Replicas, *dep.Spec.Replicas))
						}
					}

					if len(details) > 0 {
						changes = append(changes, changeInfo{
							Kind:    "Deployment",
							Name:    dep.Name,
							Action:  "update",
							Details: details,
						})
					} else {
						// Check if anything else changed by comparing YAML
						existingYAML, _ := yaml.Marshal(existing.Spec)
						newYAML, _ := yaml.Marshal(dep.Spec)
						if string(existingYAML) != string(newYAML) {
							changes = append(changes, changeInfo{
								Kind:   "Deployment",
								Name:   dep.Name,
								Action: "update",
								Detail: "spec changed",
							})
						} else {
							changes = append(changes, changeInfo{
								Kind:   "Deployment",
								Name:   dep.Name,
								Action: "unchanged",
							})
						}
					}
				}
			}

			// Print changes
			hasChanges := false
			for _, c := range changes {
				symbol := " "
				switch c.Action {
				case "create":
					symbol = "+"
					hasChanges = true
				case "update":
					symbol = "~"
					hasChanges = true
				case "delete":
					symbol = "-"
					hasChanges = true
				}

				fmt.Printf(" %s %s/%s", symbol, c.Kind, c.Name)
				if c.Action != "unchanged" {
					fmt.Printf(" (%s)", c.Action)
				}
				fmt.Println()

				if c.Detail != "" {
					fmt.Printf("     %s\n", c.Detail)
				}
				for _, d := range c.Details {
					fmt.Printf("     %s\n", d)
				}
			}

			fmt.Println()
			if !hasChanges {
				fmt.Println("No changes detected.")
			} else {
				fmt.Println("Run 'kbox deploy' to apply these changes.")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (uses overlay from kbox.yaml)")
	cmd.Flags().StringVarP(&configFile, "file", "f", "", "Path to kbox.yaml config file")

	return cmd
}

func init() {
	rootCmd.AddCommand(newDiffCmd())
}

type changeInfo struct {
	Kind    string
	Name    string
	Action  string
	Detail  string
	Details []string
}

func configMapChanged(existing, new map[string]string) bool {
	if len(existing) != len(new) {
		return true
	}
	for k, v := range new {
		if existing[k] != v {
			return true
		}
	}
	return false
}
