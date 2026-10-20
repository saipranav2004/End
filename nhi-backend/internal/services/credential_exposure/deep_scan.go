package credential_exposure

import (
	"context"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/credential_exposure"
	repo "nhi-backend-api/internal/repository/credential_exposure"
	"nhi-backend-api/internal/scope"
)

// ErrDeepScanClientNotFound is repo.ErrDeepScanClientNotFound, re-exported for the handler.
var ErrDeepScanClientNotFound = repo.ErrDeepScanClientNotFound

// DeepScanService requests and reports full-history scans. Both sides are the
// tenant's own secscan_* tables: a request writes the deep-scan row the
// scanner worker polls, and status reads it back. Nothing leaves this API.
type DeepScanService struct {
	repo     *repo.DeepScanRepository
	resolver *scope.Resolver
	now      func() time.Time
}

func NewDeepScanService(repo *repo.DeepScanRepository, resolver *scope.Resolver) *DeepScanService {
	return &DeepScanService{repo: repo, resolver: resolver, now: time.Now}
}

func (s *DeepScanService) Status(ctx context.Context, userID, tenantID, dbName, clientID, repoKey, platform string) ([]models.DeepScanResult, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	return s.repo.Status(ctx, resolved, clientID, repoKey, platform)
}

// Trigger queues the walk. The platform is the request's, else the one the
// client is known on, else - when it is known on both - read from the name:
// GitHub repositories are owner/repo, CodeCommit names cannot contain "/".
func (s *DeepScanService) Trigger(ctx context.Context, userID, tenantID, dbName string, req models.DeepScanRequest) (*models.DeepScanAccepted, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	known, err := s.repo.ClientPlatforms(ctx, resolved, req.ClientID)
	if err != nil {
		return nil, err
	}
	platform, err := pickPlatform(req.Platform, req.Repository, known)
	if err != nil {
		return nil, err
	}
	requestedAt, already, err := s.repo.Request(ctx, resolved, req.ClientID, req.Repository, platform, s.now())
	if err != nil {
		return nil, err
	}
	return &models.DeepScanAccepted{
		Status:         "running",
		ClientID:       req.ClientID,
		RepoKey:        req.Repository,
		Platform:       platform,
		RequestedAt:    requestedAt,
		AlreadyRunning: already,
	}, nil
}

func pickPlatform(asked, repository string, known []string) (string, error) {
	has := func(p string) bool {
		for _, k := range known {
			if k == p {
				return true
			}
		}
		return false
	}
	switch {
	case asked != "":
		if !has(asked) {
			return "", ErrDeepScanClientNotFound
		}
		return asked, nil
	case len(known) == 1:
		return known[0], nil
	case len(known) == 0:
		return "", ErrDeepScanClientNotFound
	case strings.Contains(repository, "/"):
		return "github", nil
	default:
		return "codecommit", nil
	}
}
