package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityGraphService struct {
	base
	repo *repo.IdentityGraphRepository
}

func NewIdentityGraphService(r *repo.IdentityGraphRepository, resolver *scope.Resolver) *IdentityGraphService {
	return &IdentityGraphService{base: newBase(resolver), repo: r}
}

func (s *IdentityGraphService) Get(ctx context.Context, c models.Caller, id string, f models.IdentityGraphFilter) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	if f.Depth < 1 {
		f.Depth = models.DefaultGraphDepth
	}
	if f.Depth > models.MaxGraphDepth {
		f.Depth = models.MaxGraphDepth
	}
	if f.MaxNodes < 1 {
		f.MaxNodes = models.DefaultGraphMaxNodes
	}
	if f.MaxNodes > models.MaxGraphMaxNodes {
		f.MaxNodes = models.MaxGraphMaxNodes
	}
	data, err := s.repo.Get(ctx, resolved, id, f)
	if err != nil {
		return nil, fmt.Errorf("get identity graph: %w", err)
	}
	return s.envelope(ctx, resolved, data, nil, "", true)
}
