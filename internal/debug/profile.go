package debug

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ProfileType represents the type of profile to collect
type ProfileType string

const (
	ProfileCPU       ProfileType = "cpu"
	ProfileHeap      ProfileType = "heap"
	ProfileGoroutine ProfileType = "goroutine"
	ProfileBlock     ProfileType = "block"
	ProfileMutex     ProfileType = "mutex"
	ProfileAllocs    ProfileType = "allocs"
)

// ProfileOptions configures profile collection
type ProfileOptions struct {
	Type       ProfileType   // Type of profile to collect
	Duration   time.Duration // Duration for CPU profiling
	Port       int           // pprof HTTP port on the pod
	OutputPath string        // Path to save the profile (auto-generated if empty)
	AppName    string        // App name for file naming
	Output     io.Writer     // For status messages
}

// DefaultProfileOptions returns sensible defaults
func DefaultProfileOptions() ProfileOptions {
	return ProfileOptions{
		Type:     ProfileCPU,
		Duration: 30 * time.Second,
		Port:     6060,
	}
}

// ProfileResult contains the result of profile collection
type ProfileResult struct {
	FilePath string
	Size     int64
	Duration time.Duration
}

// CollectProfile collects a profile from a running pod
func CollectProfile(ctx context.Context, client *kubernetes.Clientset, config *rest.Config, namespace string, pod PodInfo, opts ProfileOptions) (*ProfileResult, error) {
	result := &ProfileResult{}

	// Generate output path if not provided
	outputPath := opts.OutputPath
	if outputPath == "" {
		timestamp := time.Now().Format("20060102-150405")
		outputPath = fmt.Sprintf("%s-%s-%s.pb.gz", opts.AppName, opts.Type, timestamp)
	}

	// Ensure output directory exists
	if dir := filepath.Dir(outputPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Find an available local port
	localPort := findAvailablePort(opts.Port)

	// Set up port forwarding
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)

	fmt.Fprintf(opts.Output, "Setting up port-forward to %s:%d...\n", pod.Name, opts.Port)

	go func() {
		err := PortForward(ctx, client, config, namespace, pod.Name, PortForwardOptions{
			LocalPort:  localPort,
			RemotePort: opts.Port,
			StopCh:     stopCh,
			ReadyCh:    readyCh,
			Out:        io.Discard,
			ErrOut:     io.Discard,
		})
		if err != nil {
			errCh <- err
		}
	}()

	// Wait for port-forward to be ready
	select {
	case <-readyCh:
		// Port forward is ready
	case err := <-errCh:
		return nil, fmt.Errorf("port-forward failed: %w", err)
	case <-ctx.Done():
		close(stopCh)
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		close(stopCh)
		return nil, fmt.Errorf("timeout waiting for port-forward")
	}

	// Ensure we stop port-forwarding when done
	defer close(stopCh)

	// Build the pprof URL
	var pprofURL string
	switch opts.Type {
	case ProfileCPU:
		pprofURL = fmt.Sprintf("http://localhost:%d/debug/pprof/profile?seconds=%d", localPort, int(opts.Duration.Seconds()))
	case ProfileHeap, ProfileAllocs:
		pprofURL = fmt.Sprintf("http://localhost:%d/debug/pprof/heap", localPort)
	case ProfileGoroutine:
		pprofURL = fmt.Sprintf("http://localhost:%d/debug/pprof/goroutine", localPort)
	case ProfileBlock:
		pprofURL = fmt.Sprintf("http://localhost:%d/debug/pprof/block", localPort)
	case ProfileMutex:
		pprofURL = fmt.Sprintf("http://localhost:%d/debug/pprof/mutex", localPort)
	default:
		pprofURL = fmt.Sprintf("http://localhost:%d/debug/pprof/%s", localPort, opts.Type)
	}

	// For CPU profile, show progress
	if opts.Type == ProfileCPU {
		fmt.Fprintf(opts.Output, "Collecting CPU profile for %s...\n", opts.Duration)
		go showProgress(ctx, opts.Output, opts.Duration)
	} else {
		fmt.Fprintf(opts.Output, "Collecting %s profile...\n", opts.Type)
	}

	startTime := time.Now()

	// Fetch the profile
	httpClient := &http.Client{
		Timeout: opts.Duration + 30*time.Second, // Extra time for overhead
	}

	req, err := http.NewRequestWithContext(ctx, "GET", pprofURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch profile: %w\n\n  Ensure your app has pprof enabled:\n    import _ \"net/http/pprof\"\n    go http.ListenAndServe(\":%d\", nil)", err, opts.Port)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pprof returned status %d\n\n  Ensure your app has pprof enabled on port %d:\n    import _ \"net/http/pprof\"\n    go http.ListenAndServe(\":%d\", nil)", resp.StatusCode, opts.Port, opts.Port)
	}

	// Save to file
	outFile, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	size, err := io.Copy(outFile, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to write profile: %w", err)
	}

	result.FilePath = outputPath
	result.Size = size
	result.Duration = time.Since(startTime)

	return result, nil
}

// findAvailablePort finds an available local port, starting from the suggested port
func findAvailablePort(suggested int) int {
	// For simplicity, just use the suggested port
	// In a production implementation, we'd check if it's available
	return suggested
}

// showProgress displays a simple progress indicator
func showProgress(ctx context.Context, w io.Writer, duration time.Duration) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	elapsed := 0
	total := int(duration.Seconds())

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			elapsed++
			if elapsed > total {
				return
			}
			// Simple progress bar
			progress := float64(elapsed) / float64(total)
			barWidth := 30
			filled := int(progress * float64(barWidth))

			bar := ""
			for i := 0; i < barWidth; i++ {
				if i < filled {
					bar += "="
				} else if i == filled {
					bar += ">"
				} else {
					bar += " "
				}
			}

			fmt.Fprintf(w, "\r  [%s] %ds / %ds", bar, elapsed, total)
		}
	}
}

// FormatBytes formats bytes as human-readable
func FormatProfileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
