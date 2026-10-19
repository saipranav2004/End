package v1

import (
	"context"

	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

// PermissionSourceService resolves, on demand and for one event, which policy
// granted the permission behind it.
type PermissionSourceService struct {
	repo     *repov1.PermissionSourceRepository
	resolver *scope.Resolver
}

func NewPermissionSourceService(repo *repov1.PermissionSourceRepository, resolver *scope.Resolver) *PermissionSourceService {
	return &PermissionSourceService{repo: repo, resolver: resolver}
}

// Find takes the event as CloudTrail reports it (event_source such as
// "s3.amazonaws.com", event_name such as "GetObject").
func (s *PermissionSourceService) Find(ctx context.Context, userID, tenantID, dbName, credentialID, eventSource, eventName string) (*models.PermissionSourceResponse, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	return s.repo.Find(ctx, resolved, credentialID, chrepo.ServiceName(eventSource), eventName)
}
