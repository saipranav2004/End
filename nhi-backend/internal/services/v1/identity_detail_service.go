package v1

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/v1"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

var ErrInvalidIdentityTab = fmt.Errorf("invalid or disabled identity detail tab")

// Relationship graph expansion is deliberately disabled until every traversed
// node can be constrained to the user's account/region scope.
var validIdentityTabs = map[string]bool{
	"overview":      true,
	"permissions":   true,
	"activity":      true,
	"relationships": true,
	"credentials":   true,
	"raw":           true,
	"changes":       true,
}

var ErrIdentityNotFound = repov1.ErrIdentityNotFound

type IdentityDetailService struct {
	repo     *repov1.IdentityDetailRepository
	changes  *repov1.ScanChangesRepository
	resolver *scope.Resolver
	activity *ActivityService
}

func NewIdentityDetailService(repo *repov1.IdentityDetailRepository, changes *repov1.ScanChangesRepository, resolver *scope.Resolver, activity *ActivityService) *IdentityDetailService {
	return &IdentityDetailService{repo: repo, changes: changes, resolver: resolver, activity: activity}
}

func (s *IdentityDetailService) GetDetail(ctx context.Context, userID, tenantID, dbName string, f models.IdentityDetailFilter) (interface{}, error) {
	if !validIdentityTabs[f.Tab] {
		return nil, ErrInvalidIdentityTab
	}
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	// Normalise ?time_range= once, up front: garbage is a 400 and every tab
	// sees the same canonical label rather than the raw query string.
	tr, err := resolveRange(f.TimeRange, f.StartDate, f.EndDate)
	if err != nil {
		return nil, err
	}
	f.TimeRange = tr.Label

	src := s.activity.ForRequest(resolved)
	switch f.Tab {
	case "overview":
		return s.repo.GetOverview(ctx, resolved, f.IdentityID, src)
	case "permissions":
		return s.repo.GetPermissions(ctx, resolved, f.IdentityID, f.Service, f.UsedOnly, src)
	case "activity":
		subject, err := s.repo.GetActivitySubject(ctx, resolved, f.IdentityID)
		if err != nil {
			return nil, err
		}
		return s.activity.IdentityActivity(ctx, resolved, subject, tr, f.Service, f.Page, f.PageSize), nil
	case "relationships":
		return s.repo.GetRelationships(ctx, resolved, f.IdentityID, f.Depth, f.EdgeType, tr.Start, tr.End, src)
	case "credentials":
		return s.repo.GetCredentialsTab(ctx, resolved, f.IdentityID, src)
	case "raw":
		return s.repo.GetRaw(ctx, resolved, f.IdentityID)
	case "changes":
		subject, err := s.repo.GetActivitySubject(ctx, resolved, f.IdentityID)
		if err != nil {
			return nil, err
		}
		return entityChanges(ctx, s.changes, resolved, src, "identity_id", subject)
	default:
		return nil, ErrInvalidIdentityTab
	}
}
