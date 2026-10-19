package v1

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"nhi-backend-api/internal/database"
	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/repository"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

type SettingsService struct {
	repo     *repov1.SettingsRepository
	pg       *repository.PostgresDB
	resolver *scope.Resolver
}

func NewSettingsService(repo *repov1.SettingsRepository, pg *repository.PostgresDB, resolver *scope.Resolver) *SettingsService {
	return &SettingsService{repo: repo, pg: pg, resolver: resolver}
}

func (s *SettingsService) GetSettings(ctx context.Context, userID, tenantID, dbName string) (*models.SettingsResponse, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	return &models.SettingsResponse{
		Settings: models.SettingsBlock{
			Accounts: models.SettingsAccounts{Selected: resolved.AccountIDs, IsDefault: !resolved.HasUserSettings},
			Regions:  models.SettingsRegions{Selected: resolved.Regions, IsDefault: !resolved.HasUserSettings},
		},
		Source: sourceName(resolved.HasUserSettings),
	}, nil
}

var (
	accountIDPattern = regexp.MustCompile(`^\d{12}$`)
	// AWS inventory also uses the synthetic "global" region for global-scope resources.
	regionPattern = regexp.MustCompile(`^(global|[a-z]{2}(-[a-z]+)+-\d{1,2})$`)
)

func (s *SettingsService) UpdateSettings(ctx context.Context, userID, tenantID, dbName string, req models.UpdateSettingsRequest) (*models.UpdateSettingsResponse, error) {
	accounts, err := cleanList("selected_account_ids", req.SelectedAccountIDs, accountIDPattern)
	if err != nil {
		return nil, err
	}
	regions, err := cleanList("selected_regions", req.SelectedRegions, regionPattern)
	if err != nil {
		return nil, err
	}
	req.SelectedAccountIDs, req.SelectedRegions = accounts, regions

	tenantDB, err := s.pg.GetTenantDB(dbName)
	if err != nil {
		return nil, err
	}
	return s.repo.UpdateSettings(ctx, tenantDB, database.Table("", dbName), tenantID, userID, req)
}

func cleanList(field string, in []string, pattern *regexp.Regexp) ([]string, error) {
	if len(in) > 200 {
		return nil, fmt.Errorf("%s: at most 200 values are allowed", field)
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for i, raw := range in {
		value := strings.TrimSpace(raw)
		if !pattern.MatchString(value) {
			return nil, fmt.Errorf("%s: invalid value at position %d", field, i)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	v := value
	return &v
}

func sourceName(hasUserSettings bool) string {
	if hasUserSettings {
		return "user_settings"
	}
	return "accounts"
}
