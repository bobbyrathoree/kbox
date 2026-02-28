package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/dependencies"
	"github.com/bobbyrathoree/kbox/internal/k8s"
)

var connectCmd = &cobra.Command{
	GroupID: "observe",
	Use:     "connect <type>",
	Short:   "Connect to a dependency service",
	Long: `Connect to a dependency service with auto-fetched credentials.

Drops you into an interactive shell (e.g., psql, redis-cli, mongosh)
with credentials automatically injected from the cluster secrets.

Supported types: postgres, redis, mongodb, mysql, blob

Examples:
  kbox connect postgres              # Opens psql with auto-fetched password
  kbox connect redis                 # Opens redis-cli authenticated
  kbox connect mongodb               # Opens mongosh authenticated
  kbox connect mysql                 # Opens mysql client authenticated
  kbox connect blob                  # Shows MinIO/S3 connection info
  kbox connect postgres -a myapp     # Connect to myapp's postgres`,
	Args: exactArgs(1, "<dependency-type>"),
	RunE: runConnect,
}

func runConnect(cmd *cobra.Command, args []string) error {
	depType := strings.ToLower(args[0])

	tmpl, ok := dependencies.Get(depType)
	if !ok {
		return fmt.Errorf("unknown dependency type: %q\n  Supported: postgres, redis, mongodb, mysql, blob", depType)
	}

	// Blob storage: no interactive shell, just print connection info
	if tmpl.ConnectCommand == nil {
		fmt.Println("Blob storage connection info:")
		fmt.Println("  Endpoint: AWS_ENDPOINT_URL_S3 (injected into your app)")
		fmt.Println("  Access Key: kboxadmin")
		fmt.Println("  Bucket: kbox-default")
		fmt.Println("\nUse the AWS CLI or MinIO Client (mc) to connect:")
		fmt.Println("  aws --endpoint-url $AWS_ENDPOINT_URL_S3 s3 ls")
		return nil
	}

	// Determine app name from --app flag or kbox.yaml
	appName, _ := cmd.Flags().GetString("app")
	namespace, _ := cmd.Flags().GetString("namespace")

	if appName == "" || namespace == "" {
		loader := config.NewLoader(".")
		if cfg, err := loader.Load(); err == nil {
			if appName == "" {
				appName = cfg.Metadata.Name
			}
			if namespace == "" {
				namespace = cfg.Metadata.Namespace
			}
		}
	}

	if appName == "" {
		return fmt.Errorf("could not determine app name: use --app flag or run from a directory with kbox.yaml")
	}

	if namespace == "" {
		namespace = "default"
	}

	// Connect to cluster
	kubeContext, _ := cmd.Flags().GetString("context")
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	serviceName := fmt.Sprintf("%s-%s", appName, depType)
	ctx := context.Background()

	// Find a running pod for the dependency
	pods, err := client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", serviceName),
	})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	var pod *corev1.Pod
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			pod = &pods.Items[i]
			break
		}
	}
	if pod == nil {
		return fmt.Errorf("no running pod found for %s in namespace %s\n  Is the dependency deployed? Try: kbox status %s", serviceName, namespace, appName)
	}

	// Read secret for credentials
	secret, err := client.Clientset.CoreV1().Secrets(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to read secret %q: %w", serviceName, err)
	}

	// Build the connect command by expanding $(VAR) placeholders with secret values
	connectCmd := make([]string, len(tmpl.ConnectCommand))
	copy(connectCmd, tmpl.ConnectCommand)
	for i := range connectCmd {
		for _, key := range tmpl.SecretKeys {
			if val, ok := secret.Data[key]; ok {
				connectCmd[i] = strings.ReplaceAll(connectCmd[i], fmt.Sprintf("$(%s)", key), string(val))
			}
		}
	}

	step(fmt.Sprintf("Connecting to %s...", depType))

	_, err = debug.Shell(ctx, client.Clientset, client.RestConfig, namespace, pod.Name, debug.ShellOptions{
		Command: connectCmd,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		TTY:     true,
	})
	return err
}

func init() {
	connectCmd.Flags().StringP("app", "a", "", "App name override")
	rootCmd.AddCommand(connectCmd)
}
