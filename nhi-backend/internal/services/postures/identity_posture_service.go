package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityPostureService struct {
	base
	repo *repo.IdentityPostureRepository
}

func NewIdentityPostureService(r *repo.IdentityPostureRepository, resolver *scope.Resolver) *IdentityPostureService {
	return &IdentityPostureService{base: newBase(resolver), repo: r}
}

func (s *IdentityPostureService) Get(ctx context.Context, c models.Caller, id string, w models.Window) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	data, err := s.repo.Get(ctx, resolved, id, w)
	if err != nil {
		return nil, fmt.Errorf("get identity posture: %w", err)
	}
	return s.envelope(ctx, resolved, data, nil, w.Label, true)
}
