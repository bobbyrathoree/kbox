package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/bobbyrathoree/kbox/internal/config"
	"github.com/bobbyrathoree/kbox/internal/graph"
)

var graphCmd = &cobra.Command{
	Use:   "graph",
	Short: "Visualize app topology",
	Long: `Display application topology as ASCII art or Mermaid diagram.

Shows the relationships between:
  - Ingress -> Service -> Deployment
  - Deployments -> Dependencies (postgres, redis, etc.)
  - Multi-service dependencies (via dependsOn)
  - Volumes and config references

Output formats:
  - ASCII art (default, colorized for terminal)
  - Mermaid diagram (--format mermaid)
  - Browser view (--web)

Examples:
  kbox graph                    # ASCII topology in terminal
  kbox graph --web              # Open Mermaid diagram in browser
  kbox graph --format mermaid   # Output Mermaid code to stdout
  kbox graph --no-color         # ASCII without colors`,
	RunE: runGraph,
}

func init() {
	graphCmd.Flags().Bool("web", false, "Open Mermaid diagram in browser")
	graphCmd.Flags().String("format", "ascii", "Output format: ascii, mermaid")
	graphCmd.Flags().Bool("no-color", false, "Disable color output")
	rootCmd.AddCommand(graphCmd)
}

func runGraph(cmd *cobra.Command, args []string) error {
	webMode, _ := cmd.Flags().GetBool("web")
	format, _ := cmd.Flags().GetString("format")
	noColor, _ := cmd.Flags().GetBool("no-color")

	// Build topology from config
	topology, err := buildTopologyFromConfig()
	if err != nil {
		return err
	}

	// Handle output format
	if webMode {
		return openGraphInBrowser(topology)
	}

	switch format {
	case "mermaid":
		renderer := graph.NewMermaidRenderer(os.Stdout, topology)
		return renderer.Render()
	case "ascii":
		fallthrough
	default:
		renderer := graph.NewASCIIRenderer(os.Stdout, topology)
		renderer.SetNoColor(noColor)
		return renderer.Render()
	}
}

func buildTopologyFromConfig() (*graph.Topology, error) {
	loader := config.NewLoader(".")

	// Check if multi-service
	isMulti, err := loader.IsMultiService()
	if err != nil {
		return nil, fmt.Errorf("no kbox.yaml found\n  -> Run 'kbox init' to create one")
	}

	if isMulti {
		cfg, err := loader.LoadMultiService()
		if err != nil {
			return nil, fmt.Errorf("failed to load kbox.yaml: %w", err)
		}
		return graph.BuildFromMultiConfig(cfg)
	}

	cfg, err := loader.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load kbox.yaml: %w", err)
	}
	return graph.BuildFromConfig(cfg)
}

func openGraphInBrowser(topology *graph.Topology) error {
	// Generate HTML
	renderer := graph.NewMermaidRenderer(nil, topology)
	html := renderer.RenderHTML()

	// Write to temp file
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("kbox-graph-%s.html", topology.AppName))
	if err := os.WriteFile(tmpFile, []byte(html), 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Open in browser
	var openCmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		openCmd = exec.Command("open", tmpFile)
	case "linux":
		openCmd = exec.Command("xdg-open", tmpFile)
	case "windows":
		openCmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", tmpFile)
	default:
		fmt.Printf("Open this file in your browser: %s\n", tmpFile)
		return nil
	}

	fmt.Fprintf(os.Stderr, "Opening %s in browser...\n", tmpFile)
	return openCmd.Start()
}
