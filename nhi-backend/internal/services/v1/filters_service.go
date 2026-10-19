package v1

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/v1"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

type FilterService struct {
	filterRepo *repov1.FilterRepository
	resolver   *scope.Resolver
}

func NewFilterService(filterRepo *repov1.FilterRepository, resolver *scope.Resolver) *FilterService {
	return &FilterService{filterRepo: filterRepo, resolver: resolver}
}

func (s *FilterService) GetFilters(ctx context.Context, userID, tenantID, dbName string) (*models.FilterResponse, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	result, err := s.filterRepo.GetFilterOptions(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("load tenant filter options: %w", err)
	}
	return result, nil
}
