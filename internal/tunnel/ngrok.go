package tunnel

import (
	"context"
	"fmt"
	"net"
	"os"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

// NgrokProvider implements Provider using ngrok-go
type NgrokProvider struct{}

// NewNgrokProvider creates a new ngrok provider
func NewNgrokProvider() *NgrokProvider {
	return &NgrokProvider{}
}

// Name returns the provider name
func (p *NgrokProvider) Name() string {
	return "ngrok"
}

// CreateTunnel creates an ngrok tunnel
func (p *NgrokProvider) CreateTunnel(ctx context.Context, cfg Config) (Tunnel, error) {
	// Get auth token from config or environment
	authToken := cfg.AuthToken
	if authToken == "" {
		authToken = os.Getenv("NGROK_AUTHTOKEN")
	}

	// Build ngrok options
	opts := make([]ngrok.ConnectOption, 0)
	if authToken != "" {
		opts = append(opts, ngrok.WithAuthtoken(authToken))
	}

	// Create tunnel configuration with metadata
	metadata := fmt.Sprintf("kbox:%s/%s", cfg.Namespace, cfg.AppName)
	tunnelConfig := config.HTTPEndpoint(
		config.WithMetadata(metadata),
	)

	// Connect to ngrok and create listener
	listener, err := ngrok.Listen(ctx, tunnelConfig, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create ngrok tunnel: %w", err)
	}

	// Create tunnel wrapper
	tunnel := &ngrokTunnel{
		listener:  listener,
		localPort: cfg.LocalPort,
		url:       listener.URL(),
		done:      make(chan error, 1),
	}

	// Start forwarding in background
	go tunnel.forward(ctx)

	return tunnel, nil
}

// ngrokTunnel implements Tunnel for ngrok
type ngrokTunnel struct {
	listener  net.Listener
	localPort int
	url       string
	done      chan error
}

// URL returns the public URL
func (t *ngrokTunnel) URL() string {
	return t.url
}

// Wait blocks until the tunnel is closed
func (t *ngrokTunnel) Wait() error {
	return <-t.done
}

// Close shuts down the tunnel
func (t *ngrokTunnel) Close() error {
	return t.listener.Close()
}

// forward proxies connections from ngrok to local port
func (t *ngrokTunnel) forward(ctx context.Context) {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			// Check if listener was closed
			select {
			case <-ctx.Done():
				t.done <- ctx.Err()
				return
			default:
				t.done <- err
				return
			}
		}

		// Connect to local port and proxy
		go t.handleConn(conn)
	}
}

// handleConn proxies a single connection
func (t *ngrokTunnel) handleConn(ngrokConn net.Conn) {
	defer ngrokConn.Close()

	// Connect to local port
	localConn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", t.localPort))
	if err != nil {
		return
	}
	defer localConn.Close()

	// Bidirectional copy
	done := make(chan struct{}, 2)
	go func() {
		copyData(localConn, ngrokConn)
		done <- struct{}{}
	}()
	go func() {
		copyData(ngrokConn, localConn)
		done <- struct{}{}
	}()
	<-done
}

// copyData copies data between connections
func copyData(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}
