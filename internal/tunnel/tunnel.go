package tunnel

import (
	"context"
)

// Tunnel represents an active tunnel connection
type Tunnel interface {
	// URL returns the public URL for this tunnel
	URL() string

	// Wait blocks until the tunnel is closed or an error occurs
	Wait() error

	// Close shuts down the tunnel
	Close() error
}

// Config holds tunnel configuration
type Config struct {
	// LocalPort to forward traffic to
	LocalPort int

	// AuthToken for ngrok (optional, uses NGROK_AUTHTOKEN env if empty)
	AuthToken string

	// Region for the tunnel (optional)
	Region string

	// Metadata for the tunnel
	AppName   string
	Namespace string
}

// Provider creates tunnels
type Provider interface {
	// CreateTunnel creates a new tunnel with the given config
	CreateTunnel(ctx context.Context, cfg Config) (Tunnel, error)

	// Name returns the provider name (e.g., "ngrok")
	Name() string
}

// Result represents the outcome of a share operation
type Result struct {
	Success   bool   `json:"success"`
	URL       string `json:"url,omitempty"`
	LocalPort int    `json:"local_port"`
	AppName   string `json:"app"`
	Namespace string `json:"namespace"`
	Error     string `json:"error,omitempty"`
}
