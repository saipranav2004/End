package v1

import (
	"context"
	"fmt"
	"nhi-backend-api/internal/logging"

	models "nhi-backend-api/internal/models/v1"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

type DashboardOverviewService struct {
	repo     *repov1.DashboardOverviewRepository
	changes  *repov1.ScanChangesRepository
	resolver *scope.Resolver
	activity *ActivityService
}

func NewDashboardOverviewService(repo *repov1.DashboardOverviewRepository, changes *repov1.ScanChangesRepository, resolver *scope.Resolver, activity *ActivityService) *DashboardOverviewService {
	return &DashboardOverviewService{repo: repo, changes: changes, resolver: resolver, activity: activity}
}

func (s *DashboardOverviewService) GetOverview(ctx context.Context, userID, tenantID, dbName string) (*models.DashboardOverviewResponse, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.GetOverview(ctx, resolved, s.activity.ForRequest(ctx, resolved))
	if err != nil {
		return nil, fmt.Errorf("get tenant-scoped dashboard: %w", err)
	}
	// Recent changes are an extra: when they cannot be read the dashboard
	// still loads, without the card.
	if s.changes != nil {
		if rc, err := s.changes.RecentChanges(ctx, resolved); err != nil {
			logging.FromContext(ctx).Warn("dashboard recent changes failed, serving without the card", "err", err)
		} else {
			result.RecentChanges = rc
		}
	}
	return result, nil
}
