package identitydrift

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
)

// Service resolves the scoped identity and adds its latest drift score and
// dimension evidence. It never writes to discovery or pipeline tables.
type Service struct {
	lookup *common.MonitoredIdentityLookup
	repo   *Repository
}

// NewService wires the service.
func NewService(lookup *common.MonitoredIdentityLookup, repo *Repository) *Service {
	return &Service{lookup: lookup, repo: repo}
}

// Get returns the v3 identity drift response. A known identity with no drift
// run returns null scalar values and empty lists; an out-of-scope identity is
// still a 404 through the shared identity lookup.
func (s *Service) Get(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string) (*Response, error) {
	resolved, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName)
	if err != nil {
		return nil, err
	}
	out := &Response{Reasons: []string{}, Dimensions: []Dimension{}}
	row, err := s.repo.Latest(ctx, resolved.Scope, resolved.Identity.AllKeys())
	if common.ReadOK("identity drift score", err) && row != nil {
		out.Status = row.Status
		out.Drift = row.Drift
		out.RawDrift = row.RawDrift
		out.ModelConfidencePercent = row.ModelConfidence
		out.VerdictTrust = row.VerdictTrust
		out.RetrainingStatus = row.RetrainingStatus
		out.PSIMax = row.PSIMax
		out.PSIMaxDimension = row.PSIMaxDimension
		out.Reasons = row.Reasons
		if dimensions, err := s.repo.Dimensions(ctx, resolved.Scope, row.Key, row.WindowEnd); common.ReadOK("identity dimension drift", err) {
			out.Dimensions = dimensions
		}
	}
	if err := common.BudgetExceeded(ctx, "identity drift"); err != nil {
		return nil, err
	}
	return out, nil
}
