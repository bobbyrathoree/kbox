package graph

import (
	"fmt"
	"io"
	"strings"
)

// MermaidRenderer generates Mermaid.js diagram code
type MermaidRenderer struct {
	out      io.Writer
	topology *Topology
}

// NewMermaidRenderer creates a new Mermaid renderer
func NewMermaidRenderer(out io.Writer, topology *Topology) *MermaidRenderer {
	return &MermaidRenderer{
		out:      out,
		topology: topology,
	}
}

// Render outputs Mermaid diagram code
func (r *MermaidRenderer) Render() error {
	fmt.Fprintln(r.out, "graph TD")

	// Add subgraph for styling clarity
	fmt.Fprintln(r.out, "    %% Node definitions")

	// Render all nodes with their shapes
	for _, node := range r.topology.Nodes {
		shape := r.nodeShape(node)
		label := r.nodeLabel(node)
		id := r.sanitizeID(node.ID)
		class := r.nodeClass(node)

		fmt.Fprintf(r.out, "    %s%s:::%s\n", id, shape(label), class)
	}

	fmt.Fprintln(r.out)
	fmt.Fprintln(r.out, "    %% Edge definitions")

	// Render all edges
	for _, edge := range r.topology.Edges {
		arrow := r.edgeArrow(edge.EdgeType)
		fromID := r.sanitizeID(edge.From)
		toID := r.sanitizeID(edge.To)

		if edge.Label != "" {
			fmt.Fprintf(r.out, "    %s %s|%s| %s\n", fromID, arrow, edge.Label, toID)
		} else {
			fmt.Fprintf(r.out, "    %s %s %s\n", fromID, arrow, toID)
		}
	}

	fmt.Fprintln(r.out)
	r.renderStyles()

	return nil
}

// renderStyles outputs Mermaid style definitions
func (r *MermaidRenderer) renderStyles() {
	fmt.Fprintln(r.out, "    %% Styles")
	fmt.Fprintln(r.out, "    classDef ingress fill:#fef3c7,stroke:#d97706,stroke-width:2px,color:#92400e")
	fmt.Fprintln(r.out, "    classDef service fill:#dbeafe,stroke:#3b82f6,stroke-width:1px,color:#1e40af")
	fmt.Fprintln(r.out, "    classDef deployment fill:#cffafe,stroke:#06b6d4,stroke-width:2px,color:#0e7490")
	fmt.Fprintln(r.out, "    classDef database fill:#d1fae5,stroke:#10b981,stroke-width:2px,color:#065f46")
	fmt.Fprintln(r.out, "    classDef cache fill:#fee2e2,stroke:#ef4444,stroke-width:2px,color:#991b1b")
	fmt.Fprintln(r.out, "    classDef storage fill:#e5e7eb,stroke:#6b7280,stroke-width:1px,color:#374151")
	fmt.Fprintln(r.out, "    classDef job fill:#fef3c7,stroke:#f59e0b,stroke-width:1px,color:#92400e")
}

// nodeShape returns a function that wraps label in appropriate Mermaid shape
func (r *MermaidRenderer) nodeShape(node *Node) func(string) string {
	switch node.Type {
	case NodeTypeIngress:
		// Asymmetric shape for ingress (flag-like)
		return func(label string) string { return fmt.Sprintf(">%s]", label) }
	case NodeTypeService:
		// Stadium shape for service
		return func(label string) string { return fmt.Sprintf("([%s])", label) }
	case NodeTypeDeployment:
		// Rectangle for deployment
		return func(label string) string { return fmt.Sprintf("[%s]", label) }
	case NodeTypeStatefulSet:
		// Cylinder for database
		return func(label string) string { return fmt.Sprintf("[(%s)]", label) }
	case NodeTypePVC:
		// Parallelogram for storage
		return func(label string) string { return fmt.Sprintf("[/%s/]", label) }
	case NodeTypeJob, NodeTypeCronJob:
		// Hexagon for jobs
		return func(label string) string { return fmt.Sprintf("{{%s}}", label) }
	default:
		return func(label string) string { return fmt.Sprintf("[%s]", label) }
	}
}

// nodeLabel generates the label text for a node
func (r *MermaidRenderer) nodeLabel(node *Node) string {
	parts := []string{node.Name}

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
			if len(image) > 25 {
				image = "..." + image[len(image)-22:]
			}
			parts = append(parts, image)
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

	// Join with HTML line break for Mermaid
	return strings.Join(parts, "<br/>")
}

// nodeClass returns the CSS class for styling
func (r *MermaidRenderer) nodeClass(node *Node) string {
	switch node.Type {
	case NodeTypeIngress:
		return "ingress"
	case NodeTypeService:
		return "service"
	case NodeTypeDeployment:
		return "deployment"
	case NodeTypeStatefulSet:
		// Check if it's a cache
		if depType := node.Metadata["type"]; depType == "redis" {
			return "cache"
		}
		return "database"
	case NodeTypePVC:
		return "storage"
	case NodeTypeJob, NodeTypeCronJob:
		return "job"
	default:
		return "deployment"
	}
}

// edgeArrow returns the Mermaid arrow for an edge type
func (r *MermaidRenderer) edgeArrow(edgeType EdgeType) string {
	switch edgeType {
	case EdgeTypeRoutes:
		return "==>" // Thick arrow for traffic flow
	case EdgeTypeExposes:
		return "-->" // Normal arrow
	case EdgeTypeDependsOn:
		return "-.->" // Dotted for dependencies
	case EdgeTypeMounts:
		return "-.->" // Dotted for mounts
	case EdgeTypeUses:
		return "-->" // Normal for db usage
	default:
		return "-->"
	}
}

// sanitizeID makes node IDs safe for Mermaid
func (r *MermaidRenderer) sanitizeID(id string) string {
	// Replace / and - with _
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, "-", "_")
	// Remove other special chars
	id = strings.ReplaceAll(id, ".", "_")
	return id
}

// RenderHTML generates a complete HTML page with embedded Mermaid diagram
func (r *MermaidRenderer) RenderHTML() string {
	var mermaidCode strings.Builder
	originalOut := r.out
	r.out = &mermaidCode
	r.Render()
	r.out = originalOut

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>%s Topology - kbox</title>
    <script src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #1e293b 0%%, #0f172a 100%%);
            min-height: 100vh;
            padding: 40px;
            color: #e2e8f0;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        h1 {
            font-size: 2rem;
            font-weight: 600;
            margin-bottom: 8px;
            color: #38bdf8;
        }
        .subtitle {
            color: #94a3b8;
            margin-bottom: 32px;
        }
        .mermaid {
            background: #fff;
            border-radius: 12px;
            padding: 32px;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
        }
        .footer {
            margin-top: 24px;
            text-align: center;
            color: #64748b;
            font-size: 0.875rem;
        }
        .footer a {
            color: #38bdf8;
            text-decoration: none;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>%s</h1>
        <p class="subtitle">Namespace: %s</p>
        <div class="mermaid">
%s
        </div>
        <p class="footer">Generated by <a href="https://github.com/bobbyrathoree/kbox">kbox</a></p>
    </div>
    <script>
        mermaid.initialize({
            startOnLoad: true,
            theme: 'base',
            themeVariables: {
                primaryColor: '#0ea5e9',
                primaryTextColor: '#0f172a',
                primaryBorderColor: '#0284c7',
                lineColor: '#64748b',
                secondaryColor: '#f1f5f9',
                tertiaryColor: '#e2e8f0'
            }
        });
    </script>
</body>
</html>`, r.topology.AppName, r.topology.AppName, r.topology.Namespace, mermaidCode.String())
}
