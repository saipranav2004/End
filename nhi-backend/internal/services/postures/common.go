// Package postures is the service layer of the ISPM posture dashboard API. A
// service resolves the tenant from the caller, asks its repository for the
// data and wraps the answer in the documented envelope.
package postures

import (
	"context"
	"errors"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

// Errors the handlers translate into HTTP statuses.
var (
	ErrNotFound        = repo.ErrNotFound
	ErrInvalidCursor   = repo.ErrInvalidCursor
	ErrFindingResolved = repo.ErrFindingResolved
	ErrConflict        = repo.ErrConflict
	// ErrForbidden: the caller's role may not perform the action.
	ErrForbidden = errors.New("this role may not perform this action")
)

// ParamError is a client mistake in a named request field (HTTP 400).
type ParamError struct {
	Field   string
	Message string
}

func (e *ParamError) Error() string { return e.Message }

// base is embedded by every service: tenant resolution and scan metadata.
type base struct {
	resolver *scope.Resolver
	meta     *repo.MetaRepository
}

func newBase(resolver *scope.Resolver) base {
	return base{resolver: resolver, meta: repo.NewMetaRepository()}
}

func (b base) tenant(ctx context.Context, c models.Caller) (*scope.DataScope, error) {
	return b.resolver.ResolveTenant(ctx, c.UserID, c.TenantID, c.DBName)
}

// ScanMeta returns the latest finished scan of the caller's tenant. Handlers
// use it for the ETag before doing any heavier work.
func (b base) ScanMeta(ctx context.Context, c models.Caller) (models.Meta, error) {
	s, err := b.tenant(ctx, c)
	if err != nil {
		return models.Meta{}, err
	}
	return b.meta.ScanMeta(ctx, s)
}

// envelope wraps data with the scan metadata. available says whether the
// source data exists; the data is then empty when it is false.
func (b base) envelope(ctx context.Context, s *scope.DataScope, data any, page *models.Page, window string, available bool) (*models.Envelope, error) {
	meta, err := b.meta.ScanMeta(ctx, s)
	if err != nil {
		return nil, err
	}
	meta.Window = window
	meta.DataAvailable = available
	return &models.Envelope{Data: data, Page: page, Meta: meta}, nil
}

func pageOf(next *string, limit int) *models.Page {
	return &models.Page{NextCursor: next, Limit: limit}
}

// clampLimit applies the documented bounds: default 50, maximum 100.
func clampLimit(n int) int {
	if n < 1 {
		return 50
	}
	if n > 100 {
		return 100
	}
	return n
}
