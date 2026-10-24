package dashboarddrift

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
)

// Service resolves the caller's tenant scope and reads its current fleet
// drift snapshots. Missing or empty drift data is a successful unavailable
// response, never a fabricated zero-valued snapshot.
type Service struct {
	repo     *Repository
	resolver common.ScopeResolver
}

// NewService wires the service.
func NewService(repo *Repository, resolver common.ScopeResolver) *Service {
	return &Service{repo: repo, resolver: resolver}
}

// Get returns a bounded drift history for each currently enabled source
// family so the dashboard can render model-health trends.
func (s *Service) Get(ctx context.Context, userID, tenantID, dbName string) (*Response, error) {
	sc, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, &common.StoreUnavailableError{Op: "resolve tenant scope", Err: err}
	}
	out := &Response{Snapshots: []Snapshot{}}
	rows, err := s.repo.RecentBySource(ctx, sc)
	if common.ReadOK("dashboard drift snapshots", err) && len(rows) > 0 {
		out.DataAvailable = true
		out.Snapshots = rows
	}
	if err := common.BudgetExceeded(ctx, "dashboard drift"); err != nil {
		return nil, err
	}
	return out, nil
}
