package anomalyfeedback

import (
	"context"

	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/scope"
)

// Repository performs the single write of the genome API:
// POST /api/v1/genome/anomalies/{alert_id}/feedback upserts one
// alert_feedback row keyed by (account_id, source_type, event_id). It
// touches no other table — alerts.status is workflow state and is never
// changed here.
type Repository struct{}

// NewRepository creates the repository.
func NewRepository() *Repository { return &Repository{} }

// Write is the row to store.
type Write struct {
	AccountID           string
	SourceType          string
	EventID             string
	Decision            string // legitimate | malicious | unknown
	ApprovedForBaseline bool
	Reviewer            *string // from the access token
	Reason              *string
}

// Alert returns the alerts row with the given primary key, or nil.
func (r *Repository) Alert(ctx context.Context, resolved *scope.DataScope, alertID int64) (*common.AlertRow, error) {
	return common.AlertByID(ctx, resolved, alertID)
}

// Upsert inserts the feedback row or, when the (account_id, source_type,
// event_id) key was reviewed before, replaces decision, approved_for_baseline,
// reviewer and reason and bumps updated_at. baseline_version is left
// untouched (it is written by the pipeline's controlled baseline build,
// not by the API).
func (r *Repository) Upsert(ctx context.Context, resolved *scope.DataScope, w Write) (*common.FeedbackRow, error) {
	q := `INSERT INTO ` + resolved.Table("alert_feedback") + ` AS f
	        (account_id, source_type, event_id, decision, approved_for_baseline, reviewer, reason, created_at, updated_at)
	      VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
	      ON CONFLICT (account_id, source_type, event_id) DO UPDATE SET
	        decision = EXCLUDED.decision,
	        approved_for_baseline = EXCLUDED.approved_for_baseline,
	        reviewer = EXCLUDED.reviewer,
	        reason = EXCLUDED.reason,
	        updated_at = now()
	      RETURNING ` + common.FeedbackColumns
	row, err := common.ScanFeedback(resolved.DB.QueryRowContext(ctx, q, w.AccountID, w.SourceType, w.EventID, w.Decision, w.ApprovedForBaseline, w.Reviewer, w.Reason))
	if err != nil {
		return nil, common.WrapMonitoringErr(ctx, "alert feedback upsert", err)
	}
	return &row, nil
}
