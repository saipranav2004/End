package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type PostureExportService struct {
	base
	repo *repo.PostureExportRepository
}

func NewPostureExportService(r *repo.PostureExportRepository, resolver *scope.Resolver) *PostureExportService {
	return &PostureExportService{base: newBase(resolver), repo: r}
}

// Stream walks the filtered identities in chunks and passes each chunk to emit.
func (s *PostureExportService) Stream(ctx context.Context, c models.Caller, f models.IdentityFilter, emit func([]models.ExportRow) error) error {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return err
	}
	if err := s.repo.Stream(ctx, resolved, f, emit); err != nil {
		return fmt.Errorf("stream posture export: %w", err)
	}
	return nil
}
