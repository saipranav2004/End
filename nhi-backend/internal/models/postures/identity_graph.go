package postures

// GET /identities/{id}/graph?depth=2&edge_types=&max_nodes=200

const (
	DefaultGraphDepth    = 2
	MaxGraphDepth        = 3
	DefaultGraphMaxNodes = 200
	MaxGraphMaxNodes     = 500
)

// GraphEdgeTypes are the relationship types stored in ispm_access_edges.
var GraphEdgeTypes = []string{"BOUND_TO", "CAN_ASSUME", "TRUSTED_BY", "CROSS_ACCOUNT"}

type IdentityGraphFilter struct {
	Depth     int
	EdgeTypes []string
	MaxNodes  int
}

type IdentityGraph struct {
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	Truncated bool        `json:"truncated"`
}

type GraphNode struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Label      string   `json:"label"`
	Score      *float64 `json:"score,omitempty"`
	Privileged *bool    `json:"privileged,omitempty"`
}

type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}
