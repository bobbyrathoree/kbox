package graph

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorWhite  = "\033[37m"
	colorMuted  = "\033[90m"
	colorBold   = "\033[1m"
)

// NodeColors maps node types to colors
var NodeColors = map[NodeType]string{
	NodeTypeIngress:     colorYellow,
	NodeTypeService:     colorWhite,
	NodeTypeDeployment:  colorCyan,
	NodeTypeStatefulSet: colorGreen,
	NodeTypePVC:         colorMuted,
	NodeTypeConfigMap:   colorMuted,
	NodeTypeSecret:      colorMuted,
	NodeTypeJob:         colorYellow,
	NodeTypeCronJob:     colorYellow,
}

// DependencyColors for specific dependency types
var DependencyColors = map[string]string{
	"postgres": colorGreen,
	"mysql":    colorGreen,
	"mongodb":  colorGreen,
	"redis":    colorRed,
}

// ASCIIRenderer renders topology as ASCII art
type ASCIIRenderer struct {
	out      io.Writer
	topology *Topology
	noColor  bool
}

// NewASCIIRenderer creates a new ASCII renderer
func NewASCIIRenderer(out io.Writer, topology *Topology) *ASCIIRenderer {
	return &ASCIIRenderer{
		out:      out,
		topology: topology,
	}
}

// SetNoColor disables color output
func (r *ASCIIRenderer) SetNoColor(noColor bool) {
	r.noColor = noColor
}

// Render outputs the ASCII topology
func (r *ASCIIRenderer) Render() error {
	t := r.topology
	t.ComputeLayers()

	// Print header
	fmt.Fprintf(r.out, "%s%s%s topology %s(namespace: %s)%s\n\n",
		r.color(colorBold), r.color(colorCyan), t.AppName,
		r.color(colorMuted), t.Namespace, r.color(colorReset))

	if len(t.Nodes) == 0 {
		fmt.Fprintf(r.out, "%sNo resources found%s\n", r.color(colorMuted), r.color(colorReset))
		return nil
	}

	// Render tree structure
	r.renderTree()

	// Print legend
	fmt.Fprintln(r.out)
	r.renderLegend()

	return nil
}

// renderTree renders the topology as a tree
func (r *ASCIIRenderer) renderTree() {
	// Start from ingress or service layer
	roots := r.findRoots()

	for i, rootID := range roots {
		r.renderNode(rootID, "", i == len(roots)-1)
	}
}

// findRoots finds the top-level nodes (ingress, or services if no ingress)
func (r *ASCIIRenderer) findRoots() []string {
	t := r.topology

	// Prefer ingresses as roots
	ingresses := t.NodesByType(NodeTypeIngress)
	if len(ingresses) > 0 {
		roots := make([]string, 0, len(ingresses))
		for _, node := range ingresses {
			roots = append(roots, node.ID)
		}
		sort.Strings(roots)
		return roots
	}

	// Fall back to services
	services := t.NodesByType(NodeTypeService)
	if len(services) > 0 {
		roots := make([]string, 0, len(services))
		for _, node := range services {
			roots = append(roots, node.ID)
		}
		sort.Strings(roots)
		return roots
	}

	// Fall back to deployments
	deployments := t.NodesByType(NodeTypeDeployment)
	if len(deployments) > 0 {
		roots := make([]string, 0, len(deployments))
		for _, node := range deployments {
			roots = append(roots, node.ID)
		}
		sort.Strings(roots)
		return roots
	}

	return nil
}

// renderNode recursively renders a node and its children
func (r *ASCIIRenderer) renderNode(nodeID string, prefix string, isLast bool) {
	t := r.topology
	node := t.GetNode(nodeID)
	if node == nil {
		return
	}

	// Determine connector
	connector := "├── "
	if isLast {
		connector = "└── "
	}

	// Render node
	nodeStr := r.formatNode(node)
	fmt.Fprintf(r.out, "%s%s%s\n", prefix, connector, nodeStr)

	// Get children (nodes this node connects to)
	children := r.getChildren(nodeID)

	// Update prefix for children
	childPrefix := prefix
	if isLast {
		childPrefix += "    "
	} else {
		childPrefix += "│   "
	}

	// Render children
	for i, childID := range children {
		r.renderNode(childID, childPrefix, i == len(children)-1)
	}
}

// getChildren returns node IDs that this node connects to
func (r *ASCIIRenderer) getChildren(nodeID string) []string {
	t := r.topology
	edges := t.GetOutgoingEdges(nodeID)

	children := make([]string, 0, len(edges))
	for _, edge := range edges {
		children = append(children, edge.To)
	}

	sort.Strings(children)
	return children
}

// formatNode creates a colored string representation of a node
func (r *ASCIIRenderer) formatNode(node *Node) string {
	color := r.nodeColor(node)
	icon := r.nodeIcon(node)
	details := r.nodeDetails(node)

	name := node.Name
	if details != "" {
		return fmt.Sprintf("%s%s %s%s %s(%s)%s",
			color, icon, name, r.color(colorReset),
			r.color(colorMuted), details, r.color(colorReset))
	}
	return fmt.Sprintf("%s%s %s%s", color, icon, name, r.color(colorReset))
}

// nodeIcon returns an icon for the node type
func (r *ASCIIRenderer) nodeIcon(node *Node) string {
	switch node.Type {
	case NodeTypeIngress:
		return "[INGRESS]"
	case NodeTypeService:
		return "[SERVICE]"
	case NodeTypeDeployment:
		return "[DEPLOY]"
	case NodeTypeStatefulSet:
		// Use specific icons for known dependency types
		if depType := node.Labels["kbox.dev/dependency"]; depType != "" {
			switch depType {
			case "postgres", "mysql", "mongodb":
				return "[DB]"
			case "redis":
				return "[CACHE]"
			}
		}
		return "[STATEFULSET]"
	case NodeTypePVC:
		return "[PVC]"
	case NodeTypeJob:
		return "[JOB]"
	case NodeTypeCronJob:
		return "[CRONJOB]"
	default:
		return "[?]"
	}
}

// nodeDetails returns additional info for the node
func (r *ASCIIRenderer) nodeDetails(node *Node) string {
	parts := make([]string, 0)

	switch node.Type {
	case NodeTypeIngress:
		if host := node.Metadata["host"]; host != "" {
			parts = append(parts, host)
		}
	case NodeTypeService:
		if port := node.Metadata["port"]; port != "" {
			parts = append(parts, ":"+port)
		}
	case NodeTypeDeployment:
		if image := node.Metadata["image"]; image != "" {
			// Truncate long image names
			if len(image) > 30 {
				image = "..." + image[len(image)-27:]
			}
			parts = append(parts, image)
		}
		if replicas := node.Metadata["replicas"]; replicas != "" && replicas != "1" {
			parts = append(parts, replicas+" replicas")
		}
	case NodeTypeStatefulSet:
		if depType := node.Metadata["type"]; depType != "" {
			parts = append(parts, depType)
		}
	case NodeTypePVC:
		if storage := node.Metadata["storage"]; storage != "" {
			parts = append(parts, storage)
		}
	case NodeTypeCronJob:
		if schedule := node.Metadata["schedule"]; schedule != "" {
			parts = append(parts, schedule)
		}
	}

	return strings.Join(parts, ", ")
}

// nodeColor returns the color for a node
func (r *ASCIIRenderer) nodeColor(node *Node) string {
	// Check for dependency-specific colors first
	if depType := node.Labels["kbox.dev/dependency"]; depType != "" {
		if color, ok := DependencyColors[depType]; ok {
			return r.color(color)
		}
	}
	if depType := node.Metadata["type"]; depType != "" {
		if color, ok := DependencyColors[depType]; ok {
			return r.color(color)
		}
	}
	return r.color(NodeColors[node.Type])
}

// color returns the color code if colors are enabled
func (r *ASCIIRenderer) color(c string) string {
	if r.noColor {
		return ""
	}
	return c
}

// renderLegend prints a color legend
func (r *ASCIIRenderer) renderLegend() {
	if r.noColor {
		return
	}

	fmt.Fprintf(r.out, "%sLegend:%s ", r.color(colorMuted), r.color(colorReset))
	fmt.Fprintf(r.out, "%s[INGRESS]%s ", r.color(colorYellow), r.color(colorReset))
	fmt.Fprintf(r.out, "%s[SERVICE]%s ", r.color(colorWhite), r.color(colorReset))
	fmt.Fprintf(r.out, "%s[DEPLOY]%s ", r.color(colorCyan), r.color(colorReset))
	fmt.Fprintf(r.out, "%s[DB]%s ", r.color(colorGreen), r.color(colorReset))
	fmt.Fprintf(r.out, "%s[CACHE]%s\n", r.color(colorRed), r.color(colorReset))
}
