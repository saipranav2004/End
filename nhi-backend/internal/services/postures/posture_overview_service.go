package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type PostureOverviewService struct {
	base
	repo *repo.PostureOverviewRepository
}

func NewPostureOverviewService(r *repo.PostureOverviewRepository, resolver *scope.Resolver) *PostureOverviewService {
	return &PostureOverviewService{base: newBase(resolver), repo: r}
}

func (s *PostureOverviewService) Get(ctx context.Context, c models.Caller, w models.Window) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	data, meta, err := s.repo.Get(ctx, resolved, w)
	if err != nil {
		return nil, fmt.Errorf("get posture overview: %w", err)
	}
	return &models.Envelope{Data: data, Meta: meta}, nil
}
