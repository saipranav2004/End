package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityHeaderService struct {
	base
	repo *repo.IdentityHeaderRepository
}

func NewIdentityHeaderService(r *repo.IdentityHeaderRepository, resolver *scope.Resolver) *IdentityHeaderService {
	return &IdentityHeaderService{base: newBase(resolver), repo: r}
}

func (s *IdentityHeaderService) Get(ctx context.Context, c models.Caller, id string) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	data, err := s.repo.Get(ctx, resolved, id)
	if err != nil {
		return nil, fmt.Errorf("get identity header: %w", err)
	}
	return s.envelope(ctx, resolved, data, nil, "", true)
}
