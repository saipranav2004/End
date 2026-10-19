package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentitiesFacetsService struct {
	base
	repo *repo.IdentitiesFacetsRepository
}

func NewIdentitiesFacetsService(r *repo.IdentitiesFacetsRepository, resolver *scope.Resolver) *IdentitiesFacetsService {
	return &IdentitiesFacetsService{base: newBase(resolver), repo: r}
}

func (s *IdentitiesFacetsService) Get(ctx context.Context, c models.Caller, f models.IdentityFilter) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	data, err := s.repo.Get(ctx, resolved, f)
	if err != nil {
		return nil, fmt.Errorf("get identity facets: %w", err)
	}
	return s.envelope(ctx, resolved, data, nil, f.Window.Label, true)
}
