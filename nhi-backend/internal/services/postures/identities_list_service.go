package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentitiesListService struct {
	base
	repo *repo.IdentitiesListRepository
}

func NewIdentitiesListService(r *repo.IdentitiesListRepository, resolver *scope.Resolver) *IdentitiesListService {
	return &IdentitiesListService{base: newBase(resolver), repo: r}
}

func (s *IdentitiesListService) List(ctx context.Context, c models.Caller, f models.IdentityFilter) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	f.Limit = clampLimit(f.Limit)
	items, next, err := s.repo.List(ctx, resolved, f)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	return s.envelope(ctx, resolved, items, pageOf(next, f.Limit), f.Window.Label, true)
}
