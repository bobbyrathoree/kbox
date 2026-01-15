package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bobbyrathoree/kbox/internal/apply"
	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/render"
)

var exposeCmd = &cobra.Command{
	Use:   "expose",
	Short: "Create or update ingress for your app",
	Long: `Create an ingress to expose your app externally.

This command creates or updates an Ingress resource to make your app
accessible from outside the cluster.

Examples:
  kbox expose --host=myapp.example.com
  kbox expose --host=myapp.example.com --tls
  kbox expose --host=myapp.example.com --tls --issuer=letsencrypt-prod`,
	RunE: runExpose,
}

var unexposeCmd = &cobra.Command{
	Use:   "unexpose",
	Short: "Remove ingress from your app",
	Long:  `Delete the ingress resource for your app, making it no longer externally accessible.`,
	RunE:  runUnexpose,
}

func runExpose(cmd *cobra.Command, args []string) error {
	host, _ := cmd.Flags().GetString("host")
	path, _ := cmd.Flags().GetString("path")
	enableTLS, _ := cmd.Flags().GetBool("tls")
	issuer, _ := cmd.Flags().GetString("issuer")
	ingressClass, _ := cmd.Flags().GetString("class")
	kubeContext, _ := cmd.Flags().GetString("context")
	namespace, _ := cmd.Flags().GetString("namespace")
	ciMode := IsCIMode(cmd)
	outputFormat := GetOutputFormat(cmd)

	if host == "" {
		return fmt.Errorf("host is required\n  → Use: kbox expose --host=myapp.example.com")
	}

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Override namespace if specified
	if namespace != "" {
		cfg.Metadata.Namespace = namespace
	}

	// Update ingress config
	if cfg.Spec.Ingress == nil {
		cfg.Spec.Ingress = &config.IngressConfig{}
	}
	cfg.Spec.Ingress.Enabled = true
	cfg.Spec.Ingress.Host = host
	if path != "" {
		cfg.Spec.Ingress.Path = path
	}
	if ingressClass != "" {
		cfg.Spec.Ingress.IngressClass = ingressClass
	}

	if enableTLS {
		if cfg.Spec.Ingress.TLS == nil {
			cfg.Spec.Ingress.TLS = &config.TLSConfig{}
		}
		cfg.Spec.Ingress.TLS.Enabled = true
		if issuer != "" {
			cfg.Spec.Ingress.TLS.ClusterIssuer = issuer
		}
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context: kubeContext,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Render just the ingress
	renderer := render.New(cfg)
	ingress, err := renderer.RenderIngress()
	if err != nil {
		return fmt.Errorf("failed to render ingress: %w", err)
	}

	// Create a bundle with just the ingress
	bundle := &render.Bundle{
		Ingresses: []*networkingv1.Ingress{ingress},
	}

	if !ciMode {
		fmt.Printf("Exposing %s at %s\n", cfg.Metadata.Name, host)
	}

	// Apply
	engine := apply.NewEngine(client.Clientset, os.Stdout)
	if ciMode && outputFormat == "json" {
		engine = apply.NewEngine(client.Clientset, nil)
	}

	result, err := engine.Apply(cmd.Context(), bundle)
	if err != nil {
		return fmt.Errorf("failed to create ingress: %w", err)
	}

	// Output
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"success": len(result.Errors) == 0,
			"host":    host,
			"tls":     enableTLS,
			"created": result.Created,
			"updated": result.Updated,
		})
	}

	if !ciMode {
		fmt.Println()
		fmt.Printf("Ingress created for %s\n", host)
		if enableTLS {
			fmt.Println("  TLS enabled")
			if issuer != "" {
				fmt.Printf("  Using cert-manager issuer: %s\n", issuer)
			}
		}
		fmt.Println()
		fmt.Printf("  → Run 'kbox dns' to see DNS records to create\n")
		fmt.Printf("  → Run 'kbox unexpose' to remove the ingress\n")
	}

	return nil
}

func runUnexpose(cmd *cobra.Command, args []string) error {
	kubeContext, _ := cmd.Flags().GetString("context")
	namespace, _ := cmd.Flags().GetString("namespace")
	ciMode := IsCIMode(cmd)
	outputFormat := GetOutputFormat(cmd)

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Determine namespace
	if namespace == "" {
		namespace = cfg.Metadata.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	// Connect to cluster
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context: kubeContext,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// Delete ingress
	ingressName := cfg.Metadata.Name
	err = client.Clientset.NetworkingV1().Ingresses(namespace).Delete(cmd.Context(), ingressName, metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if !ciMode {
				fmt.Printf("No ingress found for %s\n", cfg.Metadata.Name)
			}
			return nil
		}
		return fmt.Errorf("failed to delete ingress: %w", err)
	}

	// Output
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"success": true,
			"name":    ingressName,
			"message": "Ingress deleted",
		})
	}

	if !ciMode {
		fmt.Printf("Ingress deleted for %s\n", cfg.Metadata.Name)
	}

	return nil
}

func init() {
	// Expose flags
	exposeCmd.Flags().String("host", "", "Hostname for the ingress (required)")
	exposeCmd.Flags().String("path", "/", "Path prefix for routing")
	exposeCmd.Flags().Bool("tls", false, "Enable TLS/HTTPS")
	exposeCmd.Flags().String("issuer", "", "Cert-manager ClusterIssuer for automatic TLS certificates")
	exposeCmd.Flags().String("class", "", "Ingress class (e.g., nginx, traefik)")

	rootCmd.AddCommand(exposeCmd)
	rootCmd.AddCommand(unexposeCmd)
}
