package v1

import (
	"context"
	"strings"
	"time"

	"nhi-backend-api/internal/scope"
)

// activityGraphEdges returns the relationship edges that now come from
// CloudTrail instead of trust_edges:
//
//   - "accesses": rootARN -> each resource it successfully called;
//   - "created_by": rootARN -> the principal that created it (identities only).
//
// Edges are pruned with the same scope rule as the Postgres edges (both ends
// must be in the caller's accounts and regions, and an unparseable ARN, such
// as a service name, fails closed). edgeTypes is the optional ?edge_type=
// filter; since and until bound the accessed resources to the requested time
// range. hints carries node details learned from CloudTrail (the
// resource type) for ARNs that no Postgres table knows.
func activityGraphEdges(ctx context.Context, resolved *scope.DataScope, src ActivitySource, rootARN string, keys []string, withCreator bool, edgeTypes []string, since, until time.Time) ([]graphEdgeRow, map[string]nodeInfo) {
	hints := map[string]nodeInfo{}
	if !src.Available() || rootARN == "" {
		return nil, hints
	}
	wanted := func(t string) bool {
		if len(edgeTypes) == 0 {
			return true
		}
		for _, et := range edgeTypes {
			if strings.EqualFold(et, t) {
				return true
			}
		}
		return false
	}

	var edges []graphEdgeRow
	if wanted("accesses") {
		for _, e := range src.AccessEdges(ctx, keys, since, until) {
			if e.TargetARN == "" || e.TargetARN == rootARN || !allowsAccessTarget(resolved, rootARN, e.TargetARN) {
				continue
			}
			edges = append(edges, graphEdgeRow{Source: rootARN, Target: e.TargetARN, EdgeType: "accesses", CreatedAt: e.LastObservedAt, CallCount: e.CallCount})
			hints[e.TargetARN] = nodeInfo{name: arnTail(e.TargetARN), nodeType: "resource", subtype: e.TargetType}
		}
	}
	if withCreator && wanted("created_by") {
		if creator := src.Creators(ctx, []string{rootARN})[rootARN]; creator != "" && creator != rootARN && scopeAllowsEdge(resolved, rootARN, creator) {
			// No event time is carried for the creator, so the edge has none.
			edges = append(edges, graphEdgeRow{Source: rootARN, Target: creator, EdgeType: "created_by"})
		}
	}
	return edges, hints
}

// mergeGraphEdges adds extra edges to the traversal result, skipping ones
// already present, and adds any new endpoints to the ARN list.
func mergeGraphEdges(edges []graphEdgeRow, arns []string, extra []graphEdgeRow) ([]graphEdgeRow, []string) {
	have := make(map[graphEdgeKey]bool, len(edges))
	for _, e := range edges {
		have[graphEdgeKey{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType}] = true
	}
	known := make(map[string]bool, len(arns))
	for _, a := range arns {
		known[a] = true
	}
	for _, e := range extra {
		k := graphEdgeKey{Source: e.Source, Target: e.Target, EdgeType: e.EdgeType}
		if have[k] {
			continue
		}
		have[k] = true
		edges = append(edges, e)
		for _, a := range [...]string{e.Source, e.Target} {
			if !known[a] {
				known[a] = true
				arns = append(arns, a)
			}
		}
	}
	return edges, arns
}

// callCountPtr turns an edge call count into the optional API value; edges
// that carry no count (Postgres trust edges) become nil.
func callCountPtr(n uint64) *int {
	if n == 0 {
		return nil
	}
	v := int(n)
	return &v
}

// observedAt turns an edge timestamp into the optional API value; the zero
// time (no timestamp known) becomes nil rather than year 0001.
func observedAt(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t
	return &v
}

// resourceNodeInfo derives node details from the ARN alone for ARNs that are
// neither identities nor credentials and that CloudTrail gave no hint for
// (the old resources table is gone).
func resourceNodeInfo(arn string) nodeInfo {
	if isServicePrincipal(arn) {
		return nodeInfo{name: arn, nodeType: "service", subtype: strings.TrimSuffix(strings.TrimSuffix(arn, ".cn"), ".amazonaws.com")}
	}
	parts := strings.SplitN(arn, ":", 6)
	subtype := ""
	if len(parts) == 6 {
		subtype = parts[2] // the AWS service, e.g. "ec2", "s3"
		if res := parts[5]; strings.Contains(res, "/") {
			subtype += ":" + res[:strings.Index(res, "/")]
		}
	}
	return nodeInfo{name: arnTail(arn), nodeType: "unknown", subtype: subtype}
}
