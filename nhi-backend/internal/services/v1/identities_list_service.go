package v1

import (
	"context"
	"fmt"
	"log"

	models "nhi-backend-api/internal/models/v1"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

type IdentitiesListService struct {
	repo     *repov1.IdentitiesListRepository
	resolver *scope.Resolver
	activity *ActivityService
	changes  *repov1.ScanChangesRepository
}

func NewIdentitiesListService(repo *repov1.IdentitiesListRepository, changes *repov1.ScanChangesRepository, resolver *scope.Resolver, activity *ActivityService) *IdentitiesListService {
	return &IdentitiesListService{repo: repo, changes: changes, resolver: resolver, activity: activity}
}

func (s *IdentitiesListService) GetIdentities(ctx context.Context, userID, tenantID, dbName string, f models.IdentityListFilter) (*models.IdentityListResponse, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	result, err := s.repo.GetIdentitiesList(ctx, resolved, f, s.activity.ForRequest(resolved))
	if err != nil {
		return nil, fmt.Errorf("get scoped identities: %w", err)
	}
	s.flagRecentChanges(ctx, resolved, result)
	return result, nil
}

// flagRecentChanges sets recent_change on the page's rows from the latest
// scan. Best effort: the list never fails because of it.
func (s *IdentitiesListService) flagRecentChanges(ctx context.Context, resolved *scope.DataScope, result *models.IdentityListResponse) {
	if s.changes == nil || result == nil || len(result.Data) == 0 {
		return
	}
	ids := make([]string, 0, len(result.Data))
	arns := make([]string, 0, len(result.Data))
	for _, row := range result.Data {
		ids = append(ids, row.ID)
		arns = append(arns, row.ARN)
	}
	flags, err := s.changes.RecentChangeFlags(ctx, resolved, models.EntityIdentity, ids, arns)
	if err != nil {
		log.Printf("list recent change flags: %v", err)
		return
	}
	for i := range result.Data {
		flag := flags[result.Data[i].ID]
		if flag == "" {
			flag = flags[result.Data[i].ARN]
		}
		if flag != "" {
			v := flag
			result.Data[i].RecentChange = &v
		}
	}
}
