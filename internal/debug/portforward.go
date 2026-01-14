package debug

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardOptions configures port forwarding
type PortForwardOptions struct {
	LocalPort  int
	RemotePort int
	StopCh     chan struct{}
	ReadyCh    chan struct{}
	Out        io.Writer
	ErrOut     io.Writer
}

// PortForward sets up port forwarding to a pod
func PortForward(ctx context.Context, client *kubernetes.Clientset, config *rest.Config, namespace, podName string, opts PortForwardOptions) error {
	// Build the URL for port forwarding
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	hostIP := config.Host

	u, err := url.Parse(hostIP)
	if err != nil {
		return fmt.Errorf("failed to parse host: %w", err)
	}
	u.Path = path

	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return fmt.Errorf("failed to create round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, u)

	ports := []string{fmt.Sprintf("%d:%d", opts.LocalPort, opts.RemotePort)}

	fw, err := portforward.New(dialer, ports, opts.StopCh, opts.ReadyCh, opts.Out, opts.ErrOut)
	if err != nil {
		return fmt.Errorf("failed to create port forwarder: %w", err)
	}

	return fw.ForwardPorts()
}

// ParsePort parses a port string (e.g., "8080" or "8080:3000")
func ParsePort(portStr string) (localPort, remotePort int, err error) {
	// Check for colon separator
	colonIdx := -1
	for i, c := range portStr {
		if c == ':' {
			colonIdx = i
			break
		}
	}

	if colonIdx > 0 {
		// Format: local:remote
		local, err := strconv.Atoi(portStr[:colonIdx])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid local port: %s", portStr[:colonIdx])
		}
		remote, err := strconv.Atoi(portStr[colonIdx+1:])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid remote port: %s", portStr[colonIdx+1:])
		}
		return local, remote, nil
	}

	// Single port - use same for local and remote
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid port: %s", portStr)
	}
	return port, port, nil
}
