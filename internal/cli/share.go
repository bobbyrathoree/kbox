package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/k8s"
	"github.com/bobbyrathoree/kbox/internal/tunnel"
)

var shareCmd = &cobra.Command{
	Use:   "share [app]",
	Short: "Share app via public URL",
	Long: `Create a public URL for your local Kubernetes application.

Uses ngrok to tunnel traffic from a public URL to your app running
in Kubernetes. Great for demos, testing webhooks, or sharing work.

Authentication:
  Set NGROK_AUTHTOKEN for longer sessions. Get a token at ngrok.com
  Without auth, sessions are limited to ~2 hours.

Examples:
  kbox share              # Share app from kbox.yaml
  kbox share myapp        # Share specific app
  kbox share --port 3000  # Override target port`,
	Args: cobra.MaximumNArgs(1),
	RunE: runShare,
}

func init() {
	shareCmd.Flags().IntP("port", "p", 0, "Override target port")
	shareCmd.Flags().String("token", "", "ngrok auth token (or set NGROK_AUTHTOKEN)")
	rootCmd.AddCommand(shareCmd)
}

func runShare(cmd *cobra.Command, args []string) error {
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeContext, _ := cmd.Flags().GetString("context")
	portOverride, _ := cmd.Flags().GetInt("port")
	authToken, _ := cmd.Flags().GetString("token")

	// Determine app name and port from args or config
	appName, targetPort, err := resolveShareTarget(args, portOverride)
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
		return fmt.Errorf("no pods found for %q\n  -> Is the app deployed? Try 'kbox up' first", appName)
	}

	// Pick the first ready pod
	var targetPod debug.PodInfo
	for _, p := range pods {
		if p.Ready {
			targetPod = p
			break
		}
	}
	if targetPod.Name == "" {
		targetPod = pods[0]
		fmt.Fprintf(os.Stderr, "Warning: No ready pods found, using %s anyway\n", targetPod.Name)
	}

	// Find an available local port
	localPort, err := findAvailablePort()
	if err != nil {
		return fmt.Errorf("failed to find available port: %w", err)
	}

	// Set up context with cancellation
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		cancel()
	}()

	// Start port-forward in background
	stopPF := make(chan struct{}, 1)
	readyPF := make(chan struct{})
	pfErrCh := make(chan error, 1)

	go func() {
		opts := debug.PortForwardOptions{
			LocalPort:  localPort,
			RemotePort: targetPort,
			StopCh:     stopPF,
			ReadyCh:    readyPF,
			Out:        nil, // Suppress output
			ErrOut:     nil,
		}
		pfErrCh <- debug.PortForward(ctx, client.Clientset, client.RestConfig, ns, targetPod.Name, opts)
	}()

	// Wait for port-forward to be ready
	select {
	case <-readyPF:
		// Port-forward is ready
	case err := <-pfErrCh:
		return fmt.Errorf("port-forward failed: %w", err)
	case <-ctx.Done():
		return ctx.Err()
	}

	fmt.Fprintf(os.Stderr, "Port-forward established: localhost:%d -> %s:%d\n", localPort, targetPod.Name, targetPort)
	fmt.Fprintln(os.Stderr, "Creating ngrok tunnel...")

	// Create ngrok tunnel
	provider := tunnel.NewNgrokProvider()
	tunnelCfg := tunnel.Config{
		LocalPort: localPort,
		AuthToken: authToken,
		AppName:   appName,
		Namespace: ns,
	}

	tun, err := provider.CreateTunnel(ctx, tunnelCfg)
	if err != nil {
		close(stopPF)
		return fmt.Errorf("failed to create tunnel: %w\n  -> Set NGROK_AUTHTOKEN or sign up at ngrok.com", err)
	}

	// Display the public URL
	printShareBox(tun.URL(), appName, ns)

	// Wait for tunnel to close or context cancellation
	select {
	case err := <-waitTunnel(tun):
		close(stopPF)
		if err != nil {
			return fmt.Errorf("tunnel error: %w", err)
		}
	case <-ctx.Done():
		tun.Close()
		close(stopPF)
	}

	fmt.Fprintln(os.Stderr, "Share session ended")
	return nil
}

// resolveShareTarget determines app name and port from args or config
func resolveShareTarget(args []string, portOverride int) (string, int, error) {
	loader := config.NewLoader(".")

	// If app name provided, try to get port from config
	if len(args) > 0 {
		appName := args[0]
		port := portOverride

		if port == 0 {
			// Try to get port from multi-service config
			isMulti, _ := loader.IsMultiService()
			if isMulti {
				cfg, err := loader.LoadMultiService()
				if err == nil {
					if svc, ok := cfg.Services[appName]; ok && svc.Port > 0 {
						port = svc.Port
					}
				}
			}
		}

		if port == 0 {
			port = 8080 // Default
		}

		return appName, port, nil
	}

	// No args - try to load from kbox.yaml
	isMulti, err := loader.IsMultiService()
	if err != nil {
		return "", 0, fmt.Errorf("no app specified and no kbox.yaml found\n  -> Run 'kbox share <app>' or 'kbox init' first")
	}

	if isMulti {
		cfg, err := loader.LoadMultiService()
		if err != nil {
			return "", 0, fmt.Errorf("failed to load kbox.yaml: %w", err)
		}
		if len(cfg.Services) == 0 {
			return "", 0, fmt.Errorf("no services defined in kbox.yaml")
		}
		// Use first service (get first key from map)
		var firstName string
		var firstSvc config.ServiceSpec
		for name, svc := range cfg.Services {
			firstName = name
			firstSvc = svc
			break
		}
		port := firstSvc.Port
		if portOverride > 0 {
			port = portOverride
		}
		if port == 0 {
			port = 8080
		}
		return firstName, port, nil
	}

	cfg, err := loader.Load()
	if err != nil {
		return "", 0, fmt.Errorf("failed to load kbox.yaml: %w", err)
	}

	port := cfg.Spec.Port
	if portOverride > 0 {
		port = portOverride
	}
	if port == 0 {
		port = 8080
	}

	return cfg.Metadata.Name, port, nil
}

// findAvailablePort finds an available local port
func findAvailablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// waitTunnel returns a channel that receives the tunnel wait result
func waitTunnel(tun tunnel.Tunnel) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- tun.Wait()
	}()
	return ch
}

// printShareBox displays the public URL in a nice box
func printShareBox(url, appName, namespace string) {
	// Box width
	width := 60

	// Box borders
	topBorder := "╔" + repeat("═", width-2) + "╗"
	bottomBorder := "╚" + repeat("═", width-2) + "╝"
	emptyLine := "║" + repeat(" ", width-2) + "║"

	// Center text helper
	center := func(text string) string {
		padding := (width - 2 - len(text)) / 2
		if padding < 0 {
			padding = 0
		}
		leftPad := repeat(" ", padding)
		rightPad := repeat(" ", width-2-padding-len(text))
		return "║" + leftPad + text + rightPad + "║"
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, topBorder)
	fmt.Fprintln(os.Stderr, emptyLine)
	fmt.Fprintln(os.Stderr, center(fmt.Sprintf("Sharing: %s/%s", namespace, appName)))
	fmt.Fprintln(os.Stderr, emptyLine)
	fmt.Fprintln(os.Stderr, center("Your app is now available at:"))
	fmt.Fprintln(os.Stderr, emptyLine)
	fmt.Fprintln(os.Stderr, center(url))
	fmt.Fprintln(os.Stderr, emptyLine)
	fmt.Fprintln(os.Stderr, center("Press Ctrl+C to stop sharing"))
	fmt.Fprintln(os.Stderr, emptyLine)
	fmt.Fprintln(os.Stderr, bottomBorder)
	fmt.Fprintln(os.Stderr)
}

// repeat creates a string of n repeated chars
func repeat(char string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += char
	}
	return result
}
