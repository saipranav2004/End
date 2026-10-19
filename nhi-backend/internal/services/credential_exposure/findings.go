package credential_exposure

import (
	"context"
	"fmt"
	"strings"

	models "nhi-backend-api/internal/models/credential_exposure"
	repo "nhi-backend-api/internal/repository/credential_exposure"
	"nhi-backend-api/internal/scope"
)

type FindingsService struct {
	repo     *repo.FindingsRepository
	resolver *scope.Resolver
}

func NewFindingsService(repo *repo.FindingsRepository, resolver *scope.Resolver) *FindingsService {
	return &FindingsService{repo: repo, resolver: resolver}
}

func (s *FindingsService) List(ctx context.Context, userID, tenantID, dbName string, filter models.FindingsFilter) ([]models.Finding, int, error) {
	filter.Platform = strings.ToLower(strings.TrimSpace(filter.Platform))
	if filter.Platform != "" && filter.Platform != "github" && filter.Platform != "codecommit" {
		return nil, 0, fmt.Errorf("platform must be codecommit or github")
	}
	if filter.Limit < 1 || filter.Limit > 500 {
		return nil, 0, fmt.Errorf("limit must be between 1 and 500")
	}
	if filter.Offset < 0 {
		return nil, 0, fmt.Errorf("offset cannot be negative")
	}
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.List(ctx, resolved, filter)
}

func (s *FindingsService) Get(ctx context.Context, userID, tenantID, dbName, id string) (*models.Finding, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, resolved, id)
}

func (s *FindingsService) Summary(ctx context.Context, userID, tenantID, dbName string, filter models.FindingsSummaryFilter) (models.FindingsSummary, error) {
	filter.ClientID = strings.TrimSpace(filter.ClientID)
	filter.Platform = strings.ToLower(strings.TrimSpace(filter.Platform))
	if filter.Platform != "" && filter.Platform != "github" && filter.Platform != "codecommit" {
		return models.FindingsSummary{}, fmt.Errorf("platform must be codecommit or github")
	}
	if len(filter.ClientID) > 200 {
		return models.FindingsSummary{}, fmt.Errorf("client_id exceeds 200 characters")
	}
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return models.FindingsSummary{}, err
	}
	return s.repo.Summary(ctx, resolved, filter)
}
