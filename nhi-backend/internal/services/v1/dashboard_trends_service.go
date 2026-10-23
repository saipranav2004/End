package v1

import (
	"context"

	models "nhi-backend-api/internal/models/v1"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

type DashboardTrendsService struct {
	repo     *repov1.DashboardTrendsRepository
	resolver *scope.Resolver
}

func NewDashboardTrendsService(repo *repov1.DashboardTrendsRepository, resolver *scope.Resolver) *DashboardTrendsService {
	return &DashboardTrendsService{repo: repo, resolver: resolver}
}

// TrendsQuery: empty AccountIDs/Regions mean the user's saved selection (or the
// default accounts), as for every other dashboard API.
type TrendsQuery struct {
	AccountIDs, Regions []string
	Limit               int
	Aggregate           bool
}

func (s *DashboardTrendsService) Get(ctx context.Context, userID, tenantID, dbName string, q TrendsQuery) (*models.DashboardTrendsResponse, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	accounts, err := s.repo.Accounts(ctx, resolved, q.AccountIDs, q.Regions)
	if err != nil {
		return nil, err
	}
	points, err := s.repo.Trends(ctx, resolved, accounts, q.Limit, q.Aggregate)
	if err != nil {
		return nil, err
	}
	return &models.DashboardTrendsResponse{Trends: points}, nil
}
