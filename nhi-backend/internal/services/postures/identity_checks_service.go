package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityChecksService struct {
	base
	repo *repo.IdentityChecksRepository
}

func NewIdentityChecksService(r *repo.IdentityChecksRepository, resolver *scope.Resolver) *IdentityChecksService {
	return &IdentityChecksService{base: newBase(resolver), repo: r}
}

func (s *IdentityChecksService) Get(ctx context.Context, c models.Caller, id string, f models.IdentityChecksFilter) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	if f.Status == "" {
		f.Status = models.ChecksAll
	}
	data, err := s.repo.Get(ctx, resolved, id, f)
	if err != nil {
		return nil, fmt.Errorf("get identity checks: %w", err)
	}
	return s.envelope(ctx, resolved, data, nil, "", true)
}
