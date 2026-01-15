package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/k8s"
)

var dnsCmd = &cobra.Command{
	Use:   "dns",
	Short: "Show DNS records to create for your domain",
	Long: `Show the DNS records you need to create to point your domain to your app.

This command reads your ingress configuration and shows what DNS records
you need to create with your DNS provider.`,
	Example: `  kbox dns
  kbox dns --output=json`,
	RunE: runDNS,
}

func runDNS(cmd *cobra.Command, args []string) error {
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

	// Get ingress from cluster
	ingress, err := client.Clientset.NetworkingV1().Ingresses(namespace).Get(cmd.Context(), cfg.Metadata.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("no ingress found for %s\n  → Run 'kbox expose --host=<your-domain>' first", cfg.Metadata.Name)
	}

	// Get LoadBalancer IP/hostname
	var target string
	var targetType string

	// First check ingress status for LoadBalancer info
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		lb := ingress.Status.LoadBalancer.Ingress[0]
		if lb.IP != "" {
			target = lb.IP
			targetType = "A"
		} else if lb.Hostname != "" {
			target = lb.Hostname
			targetType = "CNAME"
		}
	}

	// If no LoadBalancer status, try to get info from ingress controller service
	if target == "" {
		// Look for common ingress controller services
		for _, svcName := range []string{"ingress-nginx-controller", "nginx-ingress-controller", "traefik"} {
			for _, ns := range []string{"ingress-nginx", "nginx-ingress", "traefik", "kube-system"} {
				svc, err := client.Clientset.CoreV1().Services(ns).Get(cmd.Context(), svcName, metav1.GetOptions{})
				if err != nil {
					continue
				}
				if svc.Spec.Type == "LoadBalancer" && len(svc.Status.LoadBalancer.Ingress) > 0 {
					lb := svc.Status.LoadBalancer.Ingress[0]
					if lb.IP != "" {
						target = lb.IP
						targetType = "A"
					} else if lb.Hostname != "" {
						target = lb.Hostname
						targetType = "CNAME"
					}
					break
				}
			}
			if target != "" {
				break
			}
		}
	}

	// Collect DNS records needed
	type dnsRecord struct {
		Type   string `json:"type"`
		Name   string `json:"name"`
		Target string `json:"target"`
		TTL    int    `json:"ttl"`
	}

	var records []dnsRecord

	for _, rule := range ingress.Spec.Rules {
		if rule.Host != "" {
			record := dnsRecord{
				Type:   targetType,
				Name:   rule.Host,
				Target: target,
				TTL:    300,
			}
			if target == "" {
				record.Target = "<pending - LoadBalancer IP not yet assigned>"
				record.Type = "A or CNAME"
			}
			records = append(records, record)
		}
	}

	// Output
	if outputFormat == "json" {
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"records":    records,
			"target":     target,
			"targetType": targetType,
		})
	}

	if len(records) == 0 {
		if !ciMode {
			fmt.Println("No hosts configured in ingress")
			fmt.Println("  → Run 'kbox expose --host=<your-domain>' to configure")
		}
		return nil
	}

	if !ciMode {
		fmt.Println("Create the following DNS record(s) with your DNS provider:")
		fmt.Println()
	}

	// Table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tNAME\tTARGET\tTTL")
	for _, r := range records {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", r.Type, r.Name, r.Target, r.TTL)
	}
	w.Flush()

	if !ciMode && target == "" {
		fmt.Println()
		fmt.Println("Note: LoadBalancer IP is not yet assigned.")
		fmt.Println("  → Wait a few minutes and run 'kbox dns' again")
		fmt.Println("  → If using a cloud provider, ensure LoadBalancer provisioning is enabled")
	}

	if !ciMode && target != "" {
		fmt.Println()
		fmt.Println("After creating the DNS record:")
		fmt.Println("  → DNS propagation may take 5-30 minutes")
		fmt.Println("  → Use 'dig <hostname>' or 'nslookup <hostname>' to verify")
	}

	return nil
}

func init() {
	rootCmd.AddCommand(dnsCmd)
}
