package credential_exposure

import (
	"context"
	"strings"

	models "nhi-backend-api/internal/models/credential_exposure"
	repo "nhi-backend-api/internal/repository/credential_exposure"
	"nhi-backend-api/internal/scope"
)

// InvalidAllowlistEntryError is a client mistake in an allowlist request
// (HTTP 400). Its message is safe to return to the client.
type InvalidAllowlistEntryError struct{ Msg string }

func (e *InvalidAllowlistEntryError) Error() string { return e.Msg }

type AllowlistService struct {
	repo     *repo.AllowlistRepository
	resolver *scope.Resolver
}

func NewAllowlistService(repo *repo.AllowlistRepository, resolver *scope.Resolver) *AllowlistService {
	return &AllowlistService{repo: repo, resolver: resolver}
}

func (s *AllowlistService) List(ctx context.Context, userID, tenantID, dbName, clientID string) ([]models.AllowlistEntry, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	return s.repo.List(ctx, resolved, clientID)
}

func (s *AllowlistService) Upsert(ctx context.Context, userID, tenantID, dbName string, req models.AllowlistRequest) (*models.AllowlistEntry, error) {
	if strings.TrimSpace(req.ClientID) == "" || len(req.ClientID) > 200 || strings.TrimSpace(req.FilePath) == "" || len(req.FilePath) > 1000 || strings.TrimSpace(req.Detector) == "" || len(req.Detector) > 200 || strings.TrimSpace(req.Redacted) == "" || len(req.Redacted) > 500 {
		return nil, &InvalidAllowlistEntryError{Msg: "client_id, file_path, detector and redacted are required and must fit their database columns"}
	}
	if len(req.Reason) > 1000 {
		return nil, &InvalidAllowlistEntryError{Msg: "reason exceeds 1000 characters"}
	}
	if len(req.DismissedBy) > 200 {
		return nil, &InvalidAllowlistEntryError{Msg: "dismissed_by exceeds 200 characters"}
	}
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	return s.repo.Upsert(ctx, resolved, req)
}

func (s *AllowlistService) Delete(ctx context.Context, userID, tenantID, dbName string, req models.AllowlistRequest) (bool, error) {
	if strings.TrimSpace(req.ClientID) == "" || len(req.ClientID) > 200 || strings.TrimSpace(req.FilePath) == "" || len(req.FilePath) > 1000 || strings.TrimSpace(req.Detector) == "" || len(req.Detector) > 200 || strings.TrimSpace(req.Redacted) == "" || len(req.Redacted) > 500 {
		return false, &InvalidAllowlistEntryError{Msg: "client_id, file_path, detector and redacted are required and must fit their database columns"}
	}
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return false, err
	}
	return s.repo.Delete(ctx, resolved, req)
}
