package postures

import (
	"context"
	"fmt"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityRoleUseService struct {
	base
	repo *repo.IdentityRoleUseRepository
}

func NewIdentityRoleUseService(r *repo.IdentityRoleUseRepository, resolver *scope.Resolver) *IdentityRoleUseService {
	return &IdentityRoleUseService{base: newBase(resolver), repo: r}
}

// Get returns the Role use tab. Until the scanner provides AssumeRole events
// the envelope carries meta.data_available = false and empty lists.
func (s *IdentityRoleUseService) Get(ctx context.Context, c models.Caller, id string) (*models.Envelope, error) {
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	data, available, err := s.repo.Get(ctx, resolved, id)
	if err != nil {
		return nil, fmt.Errorf("get identity role use: %w", err)
	}
	return s.envelope(ctx, resolved, data, nil, "", available)
}
