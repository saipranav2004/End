package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type CheckDetailService struct {
	base
	repo *repo.CheckDetailRepository
}

func NewCheckDetailService(r *repo.CheckDetailRepository, resolver *scope.Resolver) *CheckDetailService {
	return &CheckDetailService{base: newBase(resolver), repo: r}
}

func (s *CheckDetailService) Get(ctx context.Context, c models.Caller, ruleID string) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	data, err := s.repo.Get(ctx, resolved, ruleID)
	if err != nil {
		return nil, fmt.Errorf("get check detail: %w", err)
	}
	return s.envelope(ctx, resolved, data, nil, "", true)
}
