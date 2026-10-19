package postures

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type FindingUpdateRepository struct{}

func NewFindingUpdateRepository() *FindingUpdateRepository {
	return &FindingUpdateRepository{}
}

// ErrFindingResolved: a RESOLVED finding can only be changed by the engine.
var ErrFindingResolved = errors.New("finding is resolved")

type findingRow struct {
	status          string
	acceptedReason  sql.NullString
	suppressedUntil sql.NullTime
	assignedTo      sql.NullString
	notes           sql.NullString
	updatedAt       time.Time
}

// Update implements PATCH /findings/{id}. Inside one transaction it locks the
// finding, applies the change, and writes
// one ispm_finding_history row per change (actor from the JWT). The identity
// score follows at the next scan, when the check's points become 0.
func (r *FindingUpdateRepository) Update(ctx context.Context, s *scope.DataScope, u models.FindingUpdate) (*models.FindingState, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin finding update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	table := s.Table(tFindings)
	var cur findingRow
	sel := fmt.Sprintf(`SELECT COALESCE(status, ''), accepted_reason, suppressed_until, assigned_to, notes, updated_at
		FROM %s WHERE tenant_id = $1 AND finding_id = $2::uuid FOR UPDATE`, table)
	err = tx.QueryRowContext(ctx, sel, s.TenantID, u.FindingID).
		Scan(&cur.status, &cur.acceptedReason, &cur.suppressedUntil, &cur.assignedTo, &cur.notes, &cur.updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock finding: %w", err)
	}

	req := u.Request
	now := time.Now().UTC()
	sets := []string{"updated_at = now()"}
	args := []any{s.TenantID, u.FindingID}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	type change struct{ action, oldValue, newValue, reason string }
	var changes []change

	if req.Status != nil {
		target := strings.ToUpper(strings.TrimSpace(*req.Status))
		if cur.status == models.FindingResolved {
			return nil, ErrFindingResolved
		}
		switch target {
		case models.FindingAccepted:
			sets = append(sets, "status = 'ACCEPTED'", "accepted_reason = "+arg(strings.TrimSpace(deref(req.AcceptedReason))), "suppressed_until = NULL")
			changes = append(changes, change{"ACCEPTED", cur.status, models.FindingAccepted, strings.TrimSpace(deref(req.AcceptedReason))})
		case models.FindingSuppressed:
			if req.SuppressedUntil == nil {
				return nil, errors.New("suppressed_until is required for SUPPRESSED")
			}
			sets = append(sets, "status = 'SUPPRESSED'", "suppressed_until = "+arg(req.SuppressedUntil.UTC()), "accepted_reason = NULL")
			changes = append(changes, change{"SUPPRESSED", cur.status, models.FindingSuppressed, "until " + req.SuppressedUntil.UTC().Format(time.RFC3339)})
		case models.FindingOpen:
			if cur.status != models.FindingOpen {
				sets = append(sets, "status = 'OPEN'", "accepted_reason = NULL", "suppressed_until = NULL")
				changes = append(changes, change{"REOPENED", cur.status, models.FindingOpen, strings.TrimSpace(deref(req.AcceptedReason))})
			}
		}
	}
	if req.AssignedTo != nil {
		assignee := strings.TrimSpace(*req.AssignedTo)
		if assignee != cur.assignedTo.String {
			if assignee == "" {
				sets = append(sets, "assigned_to = NULL")
			} else {
				sets = append(sets, "assigned_to = "+arg(assignee))
			}
			changes = append(changes, change{"ASSIGNED", cur.assignedTo.String, assignee, ""})
		}
	}
	if req.Note != nil && strings.TrimSpace(*req.Note) != "" {
		note := strings.TrimSpace(*req.Note)
		line := fmt.Sprintf("[%s %s] %s", now.Format(time.RFC3339), u.Actor, note)
		if cur.notes.Valid && strings.TrimSpace(cur.notes.String) != "" {
			line = cur.notes.String + "\n" + line
		}
		sets = append(sets, "notes = "+arg(line))
		changes = append(changes, change{"NOTE_ADDED", "", note, ""})
	}

	if len(changes) == 0 { // nothing to do: report the current state, keep updated_at
		return &models.FindingState{
			FindingID: u.FindingID, Status: cur.status, AcceptedReason: nullStr(cur.acceptedReason),
			SuppressedUntil: nullTime(cur.suppressedUntil), AssignedTo: nullStr(cur.assignedTo),
			Notes: nullStr(cur.notes), UpdatedAt: cur.updatedAt.UTC(),
		}, nil
	}

	upd := fmt.Sprintf(`UPDATE %s SET %s WHERE tenant_id = $1 AND finding_id = $2::uuid
		RETURNING COALESCE(status, ''), accepted_reason, suppressed_until, assigned_to, notes, updated_at`,
		table, strings.Join(sets, ", "))
	var out models.FindingState
	var reason, assignee, notes sql.NullString
	var until sql.NullTime
	if err := tx.QueryRowContext(ctx, upd, args...).Scan(&out.Status, &reason, &until, &assignee, &notes, &out.UpdatedAt); err != nil {
		return nil, fmt.Errorf("update finding: %w", err)
	}
	out.FindingID = u.FindingID
	out.AcceptedReason, out.AssignedTo, out.Notes = nullStr(reason), nullStr(assignee), nullStr(notes)
	out.SuppressedUntil = nullTime(until)
	out.UpdatedAt = out.UpdatedAt.UTC()

	hist := fmt.Sprintf(`INSERT INTO %s (id, finding_id, action, old_value, new_value, actor, reason, created_at)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3, $4, $5, $6, now())`, s.Table(tFindingHistory))
	for _, c := range changes {
		if _, err := tx.ExecContext(ctx, hist, u.FindingID, c.action, c.oldValue, c.newValue, u.Actor, c.reason); err != nil {
			return nil, fmt.Errorf("write finding history: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit finding update: %w", err)
	}
	return &out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
