package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityActivityService struct {
	base
	repo *repo.IdentityActivityRepository
}

func NewIdentityActivityService(r *repo.IdentityActivityRepository, resolver *scope.Resolver) *IdentityActivityService {
	return &IdentityActivityService{base: newBase(resolver), repo: r}
}

// Get returns the Activity tab. While the event store is unavailable the
// envelope carries meta.data_available = false and an empty list.
func (s *IdentityActivityService) Get(ctx context.Context, c models.Caller, id string, f models.IdentityActivityFilter) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	f.Limit = clampLimit(f.Limit)
	data, next, available, err := s.repo.Get(ctx, resolved, id, f)
	if err != nil {
		return nil, fmt.Errorf("get identity activity: %w", err)
	}
	return s.envelope(ctx, resolved, data, pageOf(next, f.Limit), f.Window.Label, available)
}
