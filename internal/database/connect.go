package database

import (
	"context"
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/bobbyrathoree/kbox/internal/debug"
	"github.com/bobbyrathoree/kbox/internal/dependencies"
)

// ConnectOptions configures database connection
type ConnectOptions struct {
	// Stdin for interactive input (default: os.Stdin)
	Stdin *os.File

	// Stdout for output (default: os.Stdout)
	Stdout *os.File

	// Stderr for errors (default: os.Stderr)
	Stderr *os.File
}

// DefaultConnectOptions returns sensible defaults
func DefaultConnectOptions() ConnectOptions {
	return ConnectOptions{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Connect opens an interactive shell to a database
func Connect(ctx context.Context, client *kubernetes.Clientset, config *rest.Config, db *DatabaseInfo, opts ConnectOptions) error {
	// Get the template for this database type
	template, ok := dependencies.Get(db.Type)
	if !ok {
		return fmt.Errorf("unsupported database type: %s", db.Type)
	}

	if len(template.ConnectCommand) == 0 {
		return fmt.Errorf("no connect command defined for %s", db.Type)
	}

	// Check if database is ready
	if !db.Ready {
		fmt.Fprintf(opts.Stderr, "Warning: database pod %s is not ready\n", db.PodName)
	}

	fmt.Fprintf(opts.Stderr, "Connecting to %s...\n", db.ServiceName)
	fmt.Fprintf(opts.Stderr, "Pod: %s\n\n", db.PodName)

	// Use debug.Shell with the connect command
	shellOpts := debug.ShellOptions{
		Command: template.ConnectCommand,
		Stdin:   opts.Stdin,
		Stdout:  opts.Stdout,
		Stderr:  opts.Stderr,
		TTY:     true,
	}

	_, err := debug.Shell(ctx, client, config, db.Namespace, db.PodName, shellOpts)
	return err
}
