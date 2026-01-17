package graph

// NodeType represents the type of resource in the topology
type NodeType string

const (
	NodeTypeIngress     NodeType = "ingress"
	NodeTypeService     NodeType = "service"
	NodeTypeDeployment  NodeType = "deployment"
	NodeTypeStatefulSet NodeType = "statefulset"
	NodeTypePVC         NodeType = "pvc"
	NodeTypeConfigMap   NodeType = "configmap"
	NodeTypeSecret      NodeType = "secret"
	NodeTypeJob         NodeType = "job"
	NodeTypeCronJob     NodeType = "cronjob"
)

// EdgeType represents the type of relationship between nodes
type EdgeType string

const (
	EdgeTypeRoutes    EdgeType = "routes"    // Ingress -> Service
	EdgeTypeExposes   EdgeType = "exposes"   // Service -> Deployment
	EdgeTypeDependsOn EdgeType = "dependsOn" // Service A -> Service B (multi-service)
	EdgeTypeMounts    EdgeType = "mounts"    // Deployment -> PVC/ConfigMap/Secret
	EdgeTypeUses      EdgeType = "uses"      // Deployment -> StatefulSet (db dependency)
)

// Node represents a resource in the topology graph
type Node struct {
	ID       string            // Unique identifier (type/name)
	Name     string            // Display name
	Type     NodeType          // Resource type
	Labels   map[string]string // K8s labels
	Metadata map[string]string // Extra info (image, port, replicas, host, etc.)
}

// Edge represents a relationship between nodes
type Edge struct {
	From     string   // Source node ID
	To       string   // Target node ID
	EdgeType EdgeType // Type of relationship
	Label    string   // Optional label (e.g., port, env var name)
}

// Topology represents the complete application graph
type Topology struct {
	AppName   string
	Namespace string
	Nodes     map[string]*Node
	Edges     []*Edge
	Layers    [][]string // Node IDs grouped by layer for rendering
}

// NewTopology creates an empty topology
func NewTopology(appName, namespace string) *Topology {
	return &Topology{
		AppName:   appName,
		Namespace: namespace,
		Nodes:     make(map[string]*Node),
		Edges:     make([]*Edge, 0),
	}
}

// AddNode adds a node to the topology
func (t *Topology) AddNode(node *Node) {
	t.Nodes[node.ID] = node
}

// AddEdge adds an edge between nodes
func (t *Topology) AddEdge(from, to string, edgeType EdgeType, label string) {
	t.Edges = append(t.Edges, &Edge{
		From:     from,
		To:       to,
		EdgeType: edgeType,
		Label:    label,
	})
}

// GetNode returns a node by ID
func (t *Topology) GetNode(id string) *Node {
	return t.Nodes[id]
}

// HasNode checks if a node exists
func (t *Topology) HasNode(id string) bool {
	_, ok := t.Nodes[id]
	return ok
}

// ComputeLayers organizes nodes into layers for rendering
// Layer 0: Ingress
// Layer 1: Services
// Layer 2: Deployments (app)
// Layer 3: StatefulSets (dependencies), PVCs
// Layer 4: Jobs, CronJobs
func (t *Topology) ComputeLayers() {
	layers := make([][]string, 5)

	for _, node := range t.Nodes {
		var layer int
		switch node.Type {
		case NodeTypeIngress:
			layer = 0
		case NodeTypeService:
			layer = 1
		case NodeTypeDeployment:
			layer = 2
		case NodeTypeStatefulSet, NodeTypePVC, NodeTypeConfigMap, NodeTypeSecret:
			layer = 3
		case NodeTypeJob, NodeTypeCronJob:
			layer = 4
		default:
			layer = 2
		}
		layers[layer] = append(layers[layer], node.ID)
	}

	// Filter empty layers
	t.Layers = make([][]string, 0)
	for _, layer := range layers {
		if len(layer) > 0 {
			t.Layers = append(t.Layers, layer)
		}
	}
}

// GetOutgoingEdges returns edges originating from a node
func (t *Topology) GetOutgoingEdges(nodeID string) []*Edge {
	result := make([]*Edge, 0)
	for _, edge := range t.Edges {
		if edge.From == nodeID {
			result = append(result, edge)
		}
	}
	return result
}

// GetIncomingEdges returns edges pointing to a node
func (t *Topology) GetIncomingEdges(nodeID string) []*Edge {
	result := make([]*Edge, 0)
	for _, edge := range t.Edges {
		if edge.To == nodeID {
			result = append(result, edge)
		}
	}
	return result
}

// NodesByType returns all nodes of a specific type
func (t *Topology) NodesByType(nodeType NodeType) []*Node {
	result := make([]*Node, 0)
	for _, node := range t.Nodes {
		if node.Type == nodeType {
			result = append(result, node)
		}
	}
	return result
}
