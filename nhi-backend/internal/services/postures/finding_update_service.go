package postures

import (
	"context"
	"fmt"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/postures"
	repo "nhi-backend-api/internal/repository/postures"
	"nhi-backend-api/internal/scope"
)

// Bounds on the free-text fields.
const (
	maxReasonLen   = 1000
	maxAssigneeLen = 254
	maxNoteLen     = 2000
)

type FindingUpdateService struct {
	base
	repo *repo.FindingUpdateRepository
}

func NewFindingUpdateService(r *repo.FindingUpdateRepository, resolver *scope.Resolver) *FindingUpdateService {
	return &FindingUpdateService{base: newBase(resolver), repo: r}
}

// Update applies PATCH /findings/{id}.
//
// Rules: only analyst / admin may write; status may be ACCEPTED, SUPPRESSED
// (needs a future suppressed_until) or OPEN — RESOLVED is set only by the
// engine; ACCEPTED needs accepted_reason; every change is written to
// ispm_finding_history with the caller as actor.
func (s *FindingUpdateService) Update(ctx context.Context, c models.Caller, u models.FindingUpdate) (*models.Envelope, error) {
	if !c.CanWrite() {
		return nil, ErrForbidden
	}
	if err := validateFindingUpdate(&u.Request, time.Now()); err != nil {
		return nil, err
	}
	resolved, err := s.tenant(ctx, c)
	if err != nil {
		return nil, err
	}
	u.Actor = c.UserID
	data, err := s.repo.Update(ctx, resolved, u)
	if err != nil {
		return nil, fmt.Errorf("update finding: %w", err)
	}
	return s.envelope(ctx, resolved, data, nil, "", true)
}

func validateFindingUpdate(r *models.FindingUpdateRequest, now time.Time) error {
	if r.Status == nil && r.AcceptedReason == nil && r.SuppressedUntil == nil && r.AssignedTo == nil && r.Note == nil {
		return &ParamError{Message: "send at least one of status, assigned_to or note"}
	}
	if r.AcceptedReason != nil && len(*r.AcceptedReason) > maxReasonLen {
		return &ParamError{Field: "accepted_reason", Message: fmt.Sprintf("accepted_reason must be at most %d characters", maxReasonLen)}
	}
	if r.AssignedTo != nil && len(*r.AssignedTo) > maxAssigneeLen {
		return &ParamError{Field: "assigned_to", Message: fmt.Sprintf("assigned_to must be at most %d characters", maxAssigneeLen)}
	}
	if r.Note != nil && len(*r.Note) > maxNoteLen {
		return &ParamError{Field: "note", Message: fmt.Sprintf("note must be at most %d characters", maxNoteLen)}
	}

	status := ""
	if r.Status != nil {
		status = strings.ToUpper(strings.TrimSpace(*r.Status))
		*r.Status = status
	}
	switch status {
	case "":
		if r.AcceptedReason != nil {
			return &ParamError{Field: "accepted_reason", Message: "accepted_reason is only valid together with status ACCEPTED"}
		}
		if r.SuppressedUntil != nil {
			return &ParamError{Field: "suppressed_until", Message: "suppressed_until is only valid together with status SUPPRESSED"}
		}
	case models.FindingAccepted:
		if r.AcceptedReason == nil || strings.TrimSpace(*r.AcceptedReason) == "" {
			return &ParamError{Field: "accepted_reason", Message: "accepted_reason is required with status ACCEPTED"}
		}
		if r.SuppressedUntil != nil {
			return &ParamError{Field: "suppressed_until", Message: "suppressed_until is only valid with status SUPPRESSED"}
		}
	case models.FindingSuppressed:
		if r.SuppressedUntil == nil || !r.SuppressedUntil.After(now) {
			return &ParamError{Field: "suppressed_until", Message: "status SUPPRESSED requires a suppressed_until in the future"}
		}
		if r.AcceptedReason != nil {
			return &ParamError{Field: "accepted_reason", Message: "accepted_reason is only valid with status ACCEPTED"}
		}
	case models.FindingOpen:
		if r.AcceptedReason != nil || r.SuppressedUntil != nil {
			return &ParamError{Field: "status", Message: "accepted_reason and suppressed_until cannot be sent with status OPEN"}
		}
	case models.FindingResolved:
		return &ParamError{Field: "status", Message: "RESOLVED is set only by the engine"}
	default:
		return &ParamError{Field: "status", Message: "status must be ACCEPTED, SUPPRESSED or OPEN"}
	}
	return nil
}
