package genome

import (
	"context"
	"errors"
	"log"

	repo "nhi-backend-api/internal/repository/genome"
	"nhi-backend-api/internal/scope"
)

// MonitoredIdentityLookup resolves the {source_type}/{identity_name} path of
// the identity endpoints (backend_api_contract.md §5.2–§5.7) to one monitored
// identity inside the caller's tenant scope. It applies the same two steps
// as the dashboard: latest non-deleted discovery snapshot per resource from
// the tenant's identities table (account/region scoped), then the §1.4
// normalizer — so an identity is addressable only by the key the other
// endpoints emit, and never outside the caller's scope.
type MonitoredIdentityLookup struct {
	discovery discoveryReader
	resolver  ScopeResolver
}

// discoveryReader is the part of DashboardSummaryRepository the lookup uses.
type discoveryReader interface {
	LatestIdentities(ctx context.Context, resolved *scope.DataScope) ([]repo.DiscoveryIdentityRow, error)
}

// NewMonitoredIdentityLookup wires the lookup (pass the shared scope.Resolver).
func NewMonitoredIdentityLookup(discovery *repo.DashboardSummaryRepository, resolver ScopeResolver) *MonitoredIdentityLookup {
	return &MonitoredIdentityLookup{discovery: discovery, resolver: resolver}
}

// ErrIdentityNotFound reports an unknown (source_type, identity_name) inside
// the caller's scope; handlers answer 404 IDENTITY_NOT_FOUND.
var ErrIdentityNotFound = errors.New("identity not found")

// resolvedIdentity is the lookup result: the tenant scope the identity was
// found in and its normalized form.
type resolvedIdentity struct {
	scope    *scope.DataScope
	identity monitoredIdentity
}

// Resolve finds the identity. Scope or discovery failures are
// *StoreUnavailableError (503); an unknown identity is ErrIdentityNotFound.
// When several snapshots normalize to the same key, the most recently seen
// one is used.
func (l *MonitoredIdentityLookup) Resolve(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string) (resolvedIdentity, error) {
	sc, fleet, err := l.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return resolvedIdentity{}, err
	}
	for _, id := range fleet {
		if id.Key.SourceType == sourceType && id.Key.IdentityName == identityName {
			return resolvedIdentity{scope: sc, identity: id}, nil
		}
	}
	return resolvedIdentity{}, ErrIdentityNotFound
}

// Fleet returns the caller's tenant scope and every monitored identity in
// it: the latest non-deleted discovery snapshot per resource, normalized,
// one entry per (source_type, identity_name) — when several snapshots
// normalize to the same key the most recently seen one wins. Order follows
// the discovery read; callers sort. Failures are *StoreUnavailableError.
func (l *MonitoredIdentityLookup) Fleet(ctx context.Context, userID, tenantID, dbName string) (*scope.DataScope, []monitoredIdentity, error) {
	sc, err := l.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, nil, &StoreUnavailableError{Op: "resolve tenant scope", Err: err}
	}
	rows, err := l.discovery.LatestIdentities(ctx, sc)
	if err != nil {
		return nil, nil, &StoreUnavailableError{Op: "read discovery identities", Err: err}
	}
	var (
		fleet []monitoredIdentity
		index = map[repo.MonitoringKey]int{}
		seen  = map[repo.MonitoringKey]*repo.DiscoveryIdentityRow{}
	)
	for i := range rows {
		id, ok := normalizeDiscoveryRow(rows[i])
		if !ok {
			continue
		}
		if at, dup := index[id.Key]; dup {
			if newerSnapshot(rows[i], seen[id.Key]) {
				fleet[at], seen[id.Key] = id, &rows[i]
			}
			continue
		}
		index[id.Key] = len(fleet)
		seen[id.Key] = &rows[i]
		fleet = append(fleet, id)
	}
	return sc, fleet, nil
}

// newerSnapshot reports whether candidate was seen more recently than current.
func newerSnapshot(candidate repo.DiscoveryIdentityRow, current *repo.DiscoveryIdentityRow) bool {
	if current == nil || current.LastSeenAt == nil {
		return candidate.LastSeenAt != nil
	}
	return candidate.LastSeenAt != nil && candidate.LastSeenAt.After(*current.LastSeenAt)
}

// APIBasePath is where the genome API is mounted in the merged backend
// (routes.GenomeBasePath under /api/v1). Its /identities would otherwise
// collide with the discovery API's /identities.
const APIBasePath = "/api/v1/genome"

// eventsReadOK reports whether a ClickHouse read succeeded; failures are
// logged and leave the dependent fields null (graceful degradation, like
// readOK for the PostgreSQL tables). A not-configured store is not logged.
func eventsReadOK(op string, err error) bool {
	if err == nil {
		return true
	}
	if !errors.Is(err, repo.ErrMonitoringEventsNotConfigured) {
		log.Printf("%s: monitoring clickhouse read failed: %v", op, err)
	}
	return false
}
