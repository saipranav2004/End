package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type QuickWinIdentitiesService struct {
	base
	repo *repo.QuickWinIdentitiesRepository
}

func NewQuickWinIdentitiesService(r *repo.QuickWinIdentitiesRepository, resolver *scope.Resolver) *QuickWinIdentitiesService {
	return &QuickWinIdentitiesService{base: newBase(resolver), repo: r}
}

func (s *QuickWinIdentitiesService) Get(ctx context.Context, c models.Caller, ruleID string, limit int, cursor string) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	limit = clampLimit(limit)
	data, next, err := s.repo.Get(ctx, resolved, ruleID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("get quick-win identities: %w", err)
	}
	return s.envelope(ctx, resolved, data, pageOf(next, limit), "", true)
}
