package cli

import (
	"fmt"
	"strings"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Pricing holds hourly costs per resource unit
type Pricing struct {
	CPUPerCore   float64
	MemoryPerGB  float64
	StoragePerGB float64
}

// defaultPricing contains approximate hourly costs for major cloud providers
// These are rough estimates based on on-demand pricing and will vary by region
var defaultPricing = map[string]Pricing{
	"aws":   {CPUPerCore: 0.0416, MemoryPerGB: 0.0052, StoragePerGB: 0.10},
	"gcp":   {CPUPerCore: 0.0350, MemoryPerGB: 0.0047, StoragePerGB: 0.08},
	"azure": {CPUPerCore: 0.0400, MemoryPerGB: 0.0050, StoragePerGB: 0.09},
}

func newCostCmd() *cobra.Command {
	var environment, provider, period string

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Estimate deployment costs",
		Long: `Estimate cloud costs based on resource requests.

Provides rough estimates based on on-demand pricing.
Actual costs vary by region, instance type, and discounts.

Supported providers: aws, gcp, azure`,
		Example: `  kbox cost                      # Estimate with defaults (AWS, monthly)
  kbox cost --provider gcp       # Use GCP pricing
  kbox cost --period hourly      # Show hourly costs
  kbox cost -e production        # Use production environment config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCost(cmd, environment, provider, period)
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Environment overlay to apply")
	cmd.Flags().StringVar(&provider, "provider", "aws", "Cloud provider (aws, gcp, azure)")
	cmd.Flags().StringVar(&period, "period", "monthly", "Cost period (hourly, daily, monthly)")

	return cmd
}

func runCost(cmd *cobra.Command, environment, provider, period string) error {
	ciMode := IsCIMode(cmd)

	// Load config
	loader := config.NewLoader(".")
	cfg, err := loader.Load()
	if err != nil {
		return fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	// Apply environment overlay if specified
	if environment != "" {
		cfg, err = cfg.ForEnvironment(environment)
		if err != nil {
			return err
		}
		if !ciMode {
			fmt.Fprintf(cmd.ErrOrStderr(), "Using environment: %s\n", environment)
		}
	}

	// Apply defaults
	cfg = cfg.WithDefaults()

	// Get pricing
	pricing, ok := defaultPricing[strings.ToLower(provider)]
	if !ok {
		return fmt.Errorf("unknown provider: %s (supported: aws, gcp, azure)", provider)
	}

	// Parse resources
	replicas := int64(cfg.Spec.Replicas)
	cpuCores := parseCPUCores(cfg.Spec.Resources)
	memoryGB := parseMemoryGB(cfg.Spec.Resources)
	storageGB := parseStorageGB(cfg)

	// Calculate hourly costs
	cpuCost := float64(replicas) * cpuCores * pricing.CPUPerCore
	memoryCost := float64(replicas) * memoryGB * pricing.MemoryPerGB
	// Storage cost is already per GB per month, divide by 730 hours
	storageCost := storageGB * pricing.StoragePerGB / 730.0

	totalHourly := cpuCost + memoryCost + storageCost

	// Determine multiplier and period name
	var multiplier float64
	var periodName string
	switch strings.ToLower(period) {
	case "hourly":
		multiplier, periodName = 1, "hour"
	case "daily":
		multiplier, periodName = 24, "day"
	default:
		multiplier, periodName = 730, "month"
	}

	totalCost := totalHourly * multiplier

	// Output
	fmt.Fprintf(cmd.OutOrStdout(), "\nCost Estimate for %s (%s)\n", cfg.Metadata.Name, strings.ToUpper(provider))
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 40))
	fmt.Fprintf(cmd.OutOrStdout(), "Replicas:         %d\n", replicas)
	fmt.Fprintf(cmd.OutOrStdout(), "CPU (per pod):    %.3f cores\n", cpuCores)
	fmt.Fprintf(cmd.OutOrStdout(), "Memory (per pod): %.3f GB\n", memoryGB)
	if storageGB > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Storage:          %.1f GB\n", storageGB)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	// Cost breakdown
	fmt.Fprintf(cmd.OutOrStdout(), "Cost breakdown (per %s):\n", periodName)
	fmt.Fprintf(cmd.OutOrStdout(), "  CPU:     $%.2f\n", cpuCost*multiplier)
	fmt.Fprintf(cmd.OutOrStdout(), "  Memory:  $%.2f\n", memoryCost*multiplier)
	if storageGB > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "  Storage: $%.2f\n", storageCost*multiplier)
	}
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 40))
	fmt.Fprintf(cmd.OutOrStdout(), "Estimated total per %s: $%.2f\n", periodName, totalCost)

	if !ciMode {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "Note: Estimates are based on on-demand pricing.")
		fmt.Fprintln(cmd.OutOrStdout(), "Actual costs vary by region, instance type, and discounts.")
	}

	return nil
}

// parseCPUCores extracts CPU cores from ResourceConfig
func parseCPUCores(res *config.ResourceConfig) float64 {
	if res == nil || res.CPU == "" {
		// Default: 100m
		return 0.1
	}
	q, err := resource.ParseQuantity(res.CPU)
	if err != nil {
		return 0.1
	}
	return float64(q.MilliValue()) / 1000.0
}

// parseMemoryGB extracts memory in GB from ResourceConfig
func parseMemoryGB(res *config.ResourceConfig) float64 {
	if res == nil || res.Memory == "" {
		// Default: 128Mi
		return 0.128
	}
	q, err := resource.ParseQuantity(res.Memory)
	if err != nil {
		return 0.128
	}
	return float64(q.Value()) / (1024 * 1024 * 1024)
}

// parseStorageGB calculates total storage from volumes and dependencies
func parseStorageGB(cfg *config.AppConfig) float64 {
	var total float64

	// App volumes with size (PVCs)
	for _, vol := range cfg.Spec.Volumes {
		if vol.Size != "" {
			if q, err := resource.ParseQuantity(vol.Size); err == nil {
				total += float64(q.Value()) / (1024 * 1024 * 1024)
			}
		}
	}

	// Dependency storage (databases, caches)
	for _, dep := range cfg.Spec.Dependencies {
		if dep.Storage != "" {
			if q, err := resource.ParseQuantity(dep.Storage); err == nil {
				total += float64(q.Value()) / (1024 * 1024 * 1024)
			}
		}
	}

	return total
}

func init() {
	rootCmd.AddCommand(newCostCmd())
}
