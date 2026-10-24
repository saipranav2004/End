package containmentfreeze

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
)

// Service implements
// POST /api/v1/genome/identities/{source_type}/{identity_name}/containment/freeze.
// Freezing an identity must not run until a containment audit log and
// worker exist, so the service resolves the identity (unknown →
// common.ErrIdentityNotFound) and then reports common.ErrNotImplemented;
// it never calls AWS and writes nothing.
type Service struct {
	lookup *common.MonitoredIdentityLookup
}

// NewService wires the service.
func NewService(lookup *common.MonitoredIdentityLookup) *Service {
	return &Service{lookup: lookup}
}

// Freeze resolves the identity and returns common.ErrNotImplemented.
func (s *Service) Freeze(ctx context.Context, userID, tenantID, dbName, sourceType, identityName string, _ Request) error {
	if _, err := s.lookup.Resolve(ctx, userID, tenantID, dbName, sourceType, identityName); err != nil {
		return err
	}
	return common.ErrNotImplemented
}
