package postures

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityGraphRepository struct{}

func NewIdentityGraphRepository() *IdentityGraphRepository {
	return &IdentityGraphRepository{}
}

// edgesPerHopLimit is a safety net on the rows read for one hop.
const edgesPerHopLimit = 5000

// Get implements GET /identities/{id}/graph: a breadth-first walk over
// ispm_access_edges, one query per hop. The walk stops at `depth` hops or at
// `max_nodes` nodes (then Truncated is set).
func (r *IdentityGraphRepository) Get(ctx context.Context, s *scope.DataScope, id string, f models.IdentityGraphFilter) (*models.IdentityGraph, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	ref, err := loadIdentityRef(ctx, s, id)
	if err != nil {
		return nil, err
	}
	g := &models.IdentityGraph{Nodes: []models.GraphNode{}, Edges: []models.GraphEdge{}}
	if ref.ARN == "" {
		return g, nil
	}

	nodeIdx := map[string]int{} // arn -> index into g.Nodes
	addNode := func(arn, typ string) {
		nodeIdx[arn] = len(g.Nodes)
		g.Nodes = append(g.Nodes, models.GraphNode{ID: arn, Type: typ, Label: arnLabel(arn)})
	}
	addNode(ref.ARN, "IDENTITY")
	seenEdge := map[string]bool{}
	frontier := []string{ref.ARN}

	for hop := 0; hop < f.Depth && len(frontier) > 0; hop++ {
		args := []any{s.TenantID, pq.Array(frontier)}
		typeCond := ""
		if len(f.EdgeTypes) > 0 {
			args = append(args, pq.Array(f.EdgeTypes))
			typeCond = " AND edge_type = ANY($3::text[])"
		}
		q := fmt.Sprintf(`SELECT source_arn, COALESCE(source_type, ''), edge_type, target_arn, COALESCE(target_type, ''),
				attributes->>'target_privileged'
			FROM %s
			WHERE tenant_id = $1 AND source_arn = ANY($2::text[])%s
			ORDER BY source_arn, edge_type, target_arn
			LIMIT %d`, s.Table(tAccessEdges), typeCond, edgesPerHopLimit)
		rows, err := s.DB.QueryContext(ctx, q, args...)
		if err != nil {
			if isUndefinedTable(err) {
				return g, nil
			}
			return nil, fmt.Errorf("query access edges: %w", err)
		}
		var next []string
		for rows.Next() {
			var src, srcType, edgeType, tgt, tgtType string
			var privileged sql.NullString
			if err := rows.Scan(&src, &srcType, &edgeType, &tgt, &tgtType, &privileged); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan access edge: %w", err)
			}
			if _, ok := nodeIdx[tgt]; !ok {
				if len(g.Nodes) >= f.MaxNodes {
					g.Truncated = true
					continue
				}
				addNode(tgt, tgtType)
				next = append(next, tgt)
			}
			ti := nodeIdx[tgt]
			if g.Nodes[ti].Type == "" {
				g.Nodes[ti].Type = tgtType
			}
			if privileged.Valid {
				v := strings.EqualFold(privileged.String, "true")
				g.Nodes[ti].Privileged = &v
			}
			if si := nodeIdx[src]; g.Nodes[si].Type == "" {
				g.Nodes[si].Type = srcType
			}
			key := src + "\x00" + edgeType + "\x00" + tgt
			if !seenEdge[key] {
				seenEdge[key] = true
				g.Edges = append(g.Edges, models.GraphEdge{Source: src, Target: tgt, Type: edgeType})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		frontier = next
	}

	return g, r.attachScores(ctx, s, g)
}

// attachScores gives identity nodes their label and score from the posture
// table (one query for the whole graph).
func (r *IdentityGraphRepository) attachScores(ctx context.Context, s *scope.DataScope, g *models.IdentityGraph) error {
	arns := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		arns = append(arns, n.ID)
	}
	q := fmt.Sprintf(`SELECT identity_arn, COALESCE(identity_name, ''), score FROM %s
		WHERE tenant_id = $1 AND identity_arn = ANY($2::text[])`, s.Table(tIdentityPosture))
	rows, err := s.DB.QueryContext(ctx, q, s.TenantID, pq.Array(arns))
	if err != nil {
		return fmt.Errorf("load graph scores: %w", err)
	}
	defer rows.Close()
	idx := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		idx[n.ID] = i
	}
	for rows.Next() {
		var arn, name string
		var score sql.NullFloat64
		if err := rows.Scan(&arn, &name, &score); err != nil {
			return fmt.Errorf("scan graph score: %w", err)
		}
		if i, ok := idx[arn]; ok {
			g.Nodes[i].Score = nullFloat(score)
			if name != "" {
				g.Nodes[i].Label = name
			}
			if g.Nodes[i].Type == "" {
				g.Nodes[i].Type = "IDENTITY"
			}
		}
	}
	return rows.Err()
}

// arnLabel is the readable tail of an ARN: the part after the last "/" (or
// the last ":" when there is no path).
func arnLabel(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 && i < len(arn)-1 {
		return arn[i+1:]
	}
	if i := strings.LastIndex(arn, ":"); i >= 0 && i < len(arn)-1 {
		return arn[i+1:]
	}
	return arn
}
