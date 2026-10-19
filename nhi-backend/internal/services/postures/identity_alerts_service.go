package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityAlertsService struct {
	base
	repo *repo.IdentityAlertsRepository
}

func NewIdentityAlertsService(r *repo.IdentityAlertsRepository, resolver *scope.Resolver) *IdentityAlertsService {
	return &IdentityAlertsService{base: newBase(resolver), repo: r}
}

func (s *IdentityAlertsService) Get(ctx context.Context, c models.Caller, id string, f models.IdentityAlertsFilter) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	if f.Status == "" {
		f.Status = models.AlertsAll
	}
	f.Limit = clampLimit(f.Limit)
	data, next, err := s.repo.Get(ctx, resolved, id, f)
	if err != nil {
		return nil, fmt.Errorf("get identity alerts: %w", err)
	}
	return s.envelope(ctx, resolved, data, pageOf(next, f.Limit), f.Window.Label, true)
}
