package identitycontainment

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
)

// Service implements
// GET /api/v1/genome/identities/{source_type}/{identity_name}/containment.
// The identity is resolved from the tenant's discovery table (unknown →
// common.ErrIdentityNotFound); its containment state is unavailable
// because the current pipeline persists no containment actions.
type Service struct {
	lookup *common.MonitoredIdentityLookup
}

// NewService wires the service.
func NewService(lookup *common.MonitoredIdentityLookup) *Service {
	return &Service{lookup: lookup}
}

// Get resolves the identity and returns the unavailable response.
func (s *Service) Get(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string) (*Response, error) {
	if _, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName); err != nil {
		return nil, err
	}
	return &Response{Available: false, Active: false, LatestAction: nil}, nil
}
