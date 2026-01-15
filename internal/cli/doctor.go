package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/spf13/cobra"
	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check your kbox setup and diagnose issues",
	Long: `Diagnose your kbox setup by checking:
  - Required tools (kubectl, docker)
  - Kubernetes connectivity and permissions
  - Optional tools (sops, kind)`,
	RunE: runDoctor,
}

type checkResult struct {
	name    string
	ok      bool
	message string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	outputFormat := GetOutputFormat(cmd)
	if outputFormat != "json" {
		fmt.Println("kbox doctor - checking your setup")
		fmt.Println()
	}

	var results []checkResult

	// Check required tools
	results = append(results, checkTool("docker", "required for building images"))
	results = append(results, checkTool("kubectl", "required for cluster operations"))

	// Check optional tools
	results = append(results, checkTool("kind", "optional, for local clusters"))
	results = append(results, checkTool("sops", "optional, for encrypted secrets"))

	// Check kubeconfig
	if k8s.HasKubeconfig() {
		results = append(results, checkResult{
			name:    "kubeconfig",
			ok:      true,
			message: k8s.KubeconfigPath(),
		})
	} else {
		results = append(results, checkResult{
			name:    "kubeconfig",
			ok:      false,
			message: "not found",
		})
	}

	// Check Kubernetes connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})

	if err != nil {
		results = append(results, checkResult{
			name:    "cluster connection",
			ok:      false,
			message: err.Error(),
		})
	} else {
		results = append(results, checkResult{
			name:    "cluster connection",
			ok:      true,
			message: fmt.Sprintf("context=%s, server=%s", client.Context, client.ServerVersion),
		})

		// Check namespace exists
		ns := client.Namespace
		if namespace != "" {
			ns = namespace
		}
		_, err := client.Clientset.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{})
		if err != nil {
			results = append(results, checkResult{
				name:    fmt.Sprintf("namespace (%s)", ns),
				ok:      false,
				message: "does not exist or no access",
			})
		} else {
			results = append(results, checkResult{
				name:    fmt.Sprintf("namespace (%s)", ns),
				ok:      true,
				message: "exists",
			})
		}

		// Check key permissions
		results = append(results, checkPermission(ctx, client, ns, "deployments", "create"))
		results = append(results, checkPermission(ctx, client, ns, "services", "create"))
		results = append(results, checkPermission(ctx, client, ns, "configmaps", "create"))
		results = append(results, checkPermission(ctx, client, ns, "pods/exec", "create"))
	}

	// Check for errors
	hasErrors := false
	for _, r := range results {
		if !r.ok {
			hasErrors = true
			break
		}
	}

	if outputFormat == "json" {
		// Build JSON-friendly result
		checks := make([]map[string]interface{}, len(results))
		for i, r := range results {
			checks[i] = map[string]interface{}{
				"name":    r.name,
				"ok":      r.ok,
				"message": r.message,
			}
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"success": !hasErrors,
			"checks":  checks,
		})
	}

	// Print text results
	fmt.Println("Results:")
	for _, r := range results {
		if r.ok {
			fmt.Printf("  ✓ %s: %s\n", r.name, r.message)
		} else {
			fmt.Printf("  ✗ %s: %s\n", r.name, r.message)
		}
	}

	fmt.Println()
	if hasErrors {
		fmt.Println("Some checks failed. Fix the issues above to use kbox effectively.")
	} else {
		fmt.Println("All checks passed. You're ready to use kbox!")
	}

	return nil
}

func checkTool(name, description string) checkResult {
	path, err := exec.LookPath(name)
	if err != nil {
		return checkResult{
			name:    name,
			ok:      false,
			message: fmt.Sprintf("not found (%s)", description),
		}
	}

	// Try to get version
	var versionArg string
	switch name {
	case "docker":
		versionArg = "--version"
	case "kubectl":
		versionArg = "version --client -o json 2>/dev/null | head -1"
	default:
		versionArg = "--version"
	}

	out, err := exec.Command(name, versionArg).Output()
	version := "found"
	if err == nil && len(out) > 0 {
		// Just take first line, truncate if needed
		version = string(out)
		if len(version) > 50 {
			version = version[:50] + "..."
		}
		// Remove newlines
		for i, c := range version {
			if c == '\n' || c == '\r' {
				version = version[:i]
				break
			}
		}
	}

	return checkResult{
		name:    name,
		ok:      true,
		message: fmt.Sprintf("%s (%s)", version, path),
	}
}

func checkPermission(ctx context.Context, client *k8s.Client, namespace, resource, verb string) checkResult {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      verb,
				Resource:  resource,
			},
		},
	}

	result, err := client.Clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return checkResult{
			name:    fmt.Sprintf("permission: %s %s", verb, resource),
			ok:      false,
			message: fmt.Sprintf("check failed: %v", err),
		}
	}

	if result.Status.Allowed {
		return checkResult{
			name:    fmt.Sprintf("permission: %s %s", verb, resource),
			ok:      true,
			message: "allowed",
		}
	}

	return checkResult{
		name:    fmt.Sprintf("permission: %s %s", verb, resource),
		ok:      false,
		message: "denied",
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
