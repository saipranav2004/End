package v1

import (
	"context"
	"errors"
	"strings"

	"nhi-backend-api/internal/scope"
)

// errScopeRequired is the single sentinel returned by every scoped repository
// entry point when it is handed a nil or unopened DataScope. Callers can test
// for it with errors.Is instead of comparing message strings.
var errScopeRequired = errors.New("tenant scope is required")

// ═══════════════════════════════════════════════════════════════════════════
// Tenant scope enforcement for the relationship graph (tab=relationships).
//
// Every other tab reads rows through DataScope.Predicates(), so the PostgreSQL
// predicate itself keeps them inside the caller's account/region selection. A
// graph traversal cannot do that: it walks trust_edges hop by hop, and hop 2
// can leave the selected accounts entirely.
//
// The rule here is therefore enforced in Go, before an edge is ever added to
// the frontier:
//
//	an edge is traversable only when BOTH endpoints resolve to an ARN whose
//	account (and, when the ARN carries one, whose region) is inside the
//	caller's resolved scope.
//
// Because the frontier only ever grows through scoped nodes, the traversal can
// never escape the tenant's selection — the graph is pruned at the boundary,
// not filtered after the fact.
//
// This fails closed: an ARN whose account id cannot be parsed is not traversable.
// Two kinds of endpoint carry no account and are handled explicitly:
//
//   - AWS service principals (lambda.amazonaws.com) trusted by an in-scope
//     role: the edge is kept, but the service is a leaf - it is never walked
//     through, or every role trusting Lambda would join one graph;
//   - account-less resource ARNs (S3 buckets: arn:aws:s3:::bucket) on an
//     "accesses" edge from CloudTrail: the call was recorded for the in-scope
//     identity, so the edge is kept (see allowsAccessTarget).
// ═══════════════════════════════════════════════════════════════════════════

// arnAccount extracts the AWS account id from an ARN.
//
//	arn:aws:iam::123456789012:role/app-role
//	      0   1  2 3          4
//
// Returns "" when the ARN is not well formed enough to yield an account id
// (for example arn:aws:iam::aws:policy/ReadOnly, which has none).
func arnAccount(arn string) string {
	parts := strings.Split(strings.TrimSpace(arn), ":")
	if len(parts) < 5 {
		return ""
	}
	return parts[4]
}

// arnRegion extracts the region from an ARN. Global services (IAM, S3,
// CloudFront) legitimately carry an empty region, in which case "" is returned
// and the region part of the scope check is skipped.
func arnRegion(arn string) string {
	parts := strings.Split(strings.TrimSpace(arn), ":")
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}

// scopeAllowsARN reports whether arn may appear in a relationship graph built
// for the given resolved scope.
func scopeAllowsARN(s *scope.DataScope, arn string) bool {
	if s == nil {
		return false
	}
	// An empty account selection means "nothing is in scope" — this mirrors
	// DataScope.Predicates(), where account_id = ANY('{}') matches no rows.
	if len(s.AccountIDs) == 0 {
		return false
	}

	account := arnAccount(arn)
	if account == "" {
		return false
	}
	if !containsString(s.AccountIDs, account) {
		return false
	}

	// Region is enforced when the ARN carries one. IAM and STS ARNs carry
	// none; the inventory records those identities under the synthetic
	// "global" region, so they follow that region here too, exactly as the
	// lists do. Other region-less ARNs (S3, CloudFront) are not filtered.
	region := arnRegion(arn)
	if region == "" && (arnService(arn) == "iam" || arnService(arn) == "sts") {
		region = globalRegion
	}
	if region != "" && len(s.Regions) > 0 {
		if !containsString(s.Regions, region) {
			return false
		}
	}
	return true
}

// globalRegion is the region the inventory records IAM identities under.
const globalRegion = "global"

// arnService extracts the service of an ARN ("iam", "s3", ...).
func arnService(arn string) string {
	parts := strings.Split(strings.TrimSpace(arn), ":")
	if len(parts) < 3 || parts[0] != "arn" {
		return ""
	}
	return parts[2]
}

// isServicePrincipal reports whether v is an AWS service principal such as
// lambda.amazonaws.com (not an ARN).
func isServicePrincipal(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "arn:") {
		return false
	}
	if !strings.HasSuffix(v, ".amazonaws.com") && !strings.HasSuffix(v, ".amazonaws.com.cn") {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// allowsAccessTarget reports whether a CloudTrail "accesses" edge from the
// in-scope root may point at target. An account-less resource ARN (an S3
// bucket or object) is allowed: the call is the root's own recorded activity.
func allowsAccessTarget(s *scope.DataScope, rootARN, target string) bool {
	if !scopeAllowsARN(s, rootARN) {
		return false
	}
	if scopeAllowsARN(s, target) {
		return true
	}
	return strings.HasPrefix(target, "arn:") && arnService(target) != "" && arnAccount(target) == ""
}

// scopeAllowsEdge reports whether a trust edge may be traversed. Both endpoints
// must be in scope; an edge that leaves the selection is dropped entirely.
func scopeAllowsEdge(s *scope.DataScope, source, target string) bool {
	return scopeAllowsARN(s, source) && scopeAllowsARN(s, target)
}

// traverseGraph walks trust_edges outward from rootARN, pruning at the tenant
// scope boundary on every hop.
//
// It returns the edges that survived scope filtering plus every ARN reached
// (root first, then in discovery order) so callers can resolve node metadata.
//
// The traversal is bounded three ways: by depth (1–5), by the caller's
// account/region scope, and by the visited set (an edge is emitted at most
// once, so a cyclic trust graph cannot loop forever).
func (r *CredentialDetailRepository) traverseGraph(
	ctx context.Context,
	resolved *scope.DataScope,
	rootARN string,
	depth int,
	edgeTypes []string,
) ([]graphEdgeRow, []string, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, nil, errScopeRequired
	}
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}

	visited := map[string]bool{rootARN: true}
	order := []string{rootARN}
	frontier := []string{rootARN}
	var allEdges []graphEdgeRow

	// loadEdgesTouching matches source_arn = ANY(frontier) OR target_arn =
	// ANY(frontier), so an edge is returned again on every hop that touches
	// either endpoint. Without de-duplication a 2-hop walk reports the same
	// edge twice, which double-counts it in the graph and in the relationship
	// table.
	seen := map[graphEdgeKey]bool{}

	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		edges, err := r.loadEdgesTouching(ctx, resolved.DB, resolved.QSchema(), frontier, edgeTypes)
		if err != nil {
			return nil, nil, err
		}
		kept, next, leaves := scopedEdges(resolved, edges, visited)
		for _, e := range kept {
			key := graphEdgeKey{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType}
			if seen[key] {
				continue
			}
			seen[key] = true
			allEdges = append(allEdges, e)
		}
		order = append(order, next...)
		order = append(order, leaves...)
		frontier = next
	}
	return allEdges, order, nil
}

// graphEdgeKey identifies an edge for de-duplication.
type graphEdgeKey struct {
	Source   string
	Target   string
	EdgeType string
}

// scopedEdges keeps only the traversable edges and returns the not-yet-visited
// endpoints as the next BFS frontier. Service principals are kept as leaves:
// returned in leaves (so their node is drawn) but never walked through.
func scopedEdges(s *scope.DataScope, edges []graphEdgeRow, visited map[string]bool) (kept []graphEdgeRow, frontier, leaves []string) {
	kept = make([]graphEdgeRow, 0, len(edges))
	for _, e := range edges {
		srcService, dstService := isServicePrincipal(e.Source), isServicePrincipal(e.Target)
		switch {
		case srcService && dstService:
			continue
		case srcService:
			if !scopeAllowsARN(s, e.Target) {
				continue
			}
		case dstService:
			if !scopeAllowsARN(s, e.Source) {
				continue
			}
		default:
			if !scopeAllowsEdge(s, e.Source, e.Target) {
				continue
			}
		}
		kept = append(kept, e)
		for _, arn := range [...]string{e.Source, e.Target} {
			if visited[arn] {
				continue
			}
			visited[arn] = true
			if isServicePrincipal(arn) {
				leaves = append(leaves, arn)
			} else {
				frontier = append(frontier, arn)
			}
		}
	}
	return kept, frontier, leaves
}

func containsString(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}

// parseEdgeTypes splits the comma-separated ?edge_type= filter. An empty or
// absent value means "every edge type".
func parseEdgeTypes(edgeType string) []string {
	var out []string
	for _, et := range strings.Split(edgeType, ",") {
		if et = strings.ToLower(strings.TrimSpace(et)); et != "" {
			out = append(out, et)
		}
	}
	return out
}

// escapeLikePattern makes a search term match literally inside ILIKE ... ESCAPE '\':
// "%" and "_" are wildcards in SQL and would otherwise match anything.
func escapeLikePattern(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}
