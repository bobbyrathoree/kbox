package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/spf13/cobra"
)

var pfCmd = &cobra.Command{
	Use:   "pf <app> <port>",
	Short: "Port-forward to an app",
	Long: `Set up port forwarding to a pod.

Unlike kubectl port-forward, kbox pf:
  - Auto-selects a ready pod
  - Uses simpler syntax
  - Shows clear connection info

Port format:
  8080       - Forward local 8080 to remote 8080
  8080:3000  - Forward local 8080 to remote 3000

Examples:
  kbox pf myapp 8080        # Forward localhost:8080 to pod:8080
  kbox pf myapp 9000:8080   # Forward localhost:9000 to pod:8080`,
	Args: cobra.ExactArgs(2),
	RunE: runPortForward,
}

func runPortForward(cmd *cobra.Command, args []string) error {
	appName := args[0]
	portSpec := args[1]

	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")

	// Parse port
	localPort, remotePort, err := debug.ParsePort(portSpec)
	if err != nil {
		return err
	}

	// Create K8s client
	client, err := k8s.NewClient(k8s.ClientOptions{
		Context:   kubeContext,
		Namespace: namespace,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	ns := client.Namespace
	if namespace != "" {
		ns = namespace
	}

	// Find pods for the app
	pods, err := debug.FindPods(cmd.Context(), client.Clientset, ns, appName)
	if err != nil {
		return err
	}

	// Pick the first ready pod, or just the first one
	var targetPod debug.PodInfo
	for _, p := range pods {
		if p.Ready {
			targetPod = p
			break
		}
	}
	if targetPod.Name == "" {
		targetPod = pods[0]
	}

	// Set up channels for port-forward lifecycle
	stopCh := make(chan struct{}, 1)
	readyCh := make(chan struct{})

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nStopping port-forward...")
		close(stopCh)
	}()

	// Print info
	fmt.Fprintf(os.Stderr, "Forwarding %s/%s\n", ns, targetPod.Name)
	fmt.Fprintf(os.Stderr, "  localhost:%d -> pod:%d\n", localPort, remotePort)
	fmt.Fprintln(os.Stderr, "Press Ctrl+C to stop")
	fmt.Fprintln(os.Stderr)

	opts := debug.PortForwardOptions{
		LocalPort:  localPort,
		RemotePort: remotePort,
		StopCh:     stopCh,
		ReadyCh:    readyCh,
		Out:        os.Stdout,
		ErrOut:     os.Stderr,
	}

	// Wait for ready in a goroutine and print when connected
	go func() {
		<-readyCh
		fmt.Fprintf(os.Stderr, "Port-forward established. Access via http://localhost:%d\n", localPort)
	}()

	return debug.PortForward(cmd.Context(), client.Clientset, client.RestConfig, ns, targetPod.Name, opts)
}

func init() {
	rootCmd.AddCommand(pfCmd)
}
