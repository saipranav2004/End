package credential_exposure

import (
	"context"
	"fmt"
	"strings"

	repo "nhi-backend-api/internal/repository/credential_exposure"
	"nhi-backend-api/internal/scope"
)

type DecisionService struct {
	repo     *repo.DecisionRepository
	resolver *scope.Resolver
}

func NewDecisionService(repo *repo.DecisionRepository, resolver *scope.Resolver) *DecisionService {
	return &DecisionService{repo: repo, resolver: resolver}
}

func (s *DecisionService) Save(ctx context.Context, userID, tenantID, dbName, id, decision, by string) error {
	id = strings.TrimSpace(id)
	decision = strings.ToLower(strings.TrimSpace(decision))
	by = strings.TrimSpace(by)
	if id == "" || len(id) > 1000 {
		return fmt.Errorf("finding_id is required and must not exceed 1000 characters")
	}
	if decision != "real_secret" && decision != "false_positive" {
		return fmt.Errorf("decision must be real_secret or false_positive")
	}
	if by == "" {
		by = "dashboard user"
	}
	if len(by) > 300 {
		return fmt.Errorf("decided_by exceeds 300 characters")
	}
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return err
	}
	return s.repo.Save(ctx, resolved, id, decision, by)
}
