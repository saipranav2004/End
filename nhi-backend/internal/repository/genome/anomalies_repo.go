package genome

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"nhi-backend-api/internal/scope"
)

// AnomaliesRepository reads alerts with their reviewer feedback and writes
// that feedback (backend-nhi-genome.md §6.1, §6.4). alerts and alert_feedback
// belong to the behaviour pipeline: this API only ever adds or updates the
// one current alert_feedback row of an alert's (source_type, event_id); it
// never changes alerts.status or a baseline.
type AnomaliesRepository struct{}

// NewAnomaliesRepository creates the repository.
func NewAnomaliesRepository() *AnomaliesRepository { return &AnomaliesRepository{} }

// ErrAlertNotFound is an alert id that is not in the caller's tenant.
var ErrAlertNotFound = errors.New("alert not found")

// AnomalyFilter narrows the alert rows. Empty strings and nil times mean
// "no filter". Review is "", "pending" or "reviewed".
type AnomalyFilter struct {
	Dimension string
	State     string
	From, To  *time.Time
	Review    string
}

// AnomalyCursor resumes the created_at DESC, id DESC order.
type AnomalyCursor struct {
	CreatedAt time.Time
	ID        int64
}

// AlertRow is one alert with its current feedback (nil when none).
type AlertRow struct {
	ID        int64
	Key       MonitoringKey
	Dimension string
	Reason    string
	Status    string
	CreatedAt time.Time
	EventID   string
	Observed  *string
	Baseline  *string
	Feedback  *FeedbackRow
}

// FeedbackRow is the current alert_feedback row of an alert.
type FeedbackRow struct {
	Decision            string
	ApprovedForBaseline bool
	Reviewer            string
	Reason              *string
	UpdatedAt           time.Time
}

// AnomalyCountsRow are the review counts of the filtered alerts.
type AnomalyCountsRow struct {
	Total, Pending, Malicious, Legitimate int
}

// FeedbackTableExists reports whether the tenant has an alert_feedback table.
func (r *AnomaliesRepository) FeedbackTableExists(ctx context.Context, resolved *scope.DataScope) (bool, error) {
	var ok bool
	err := resolved.DB.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, resolved.Schema+".alert_feedback").Scan(&ok)
	return ok, wrapMonitoringErr("alert_feedback lookup", err)
}

// feedbackJoin is the current feedback row of each alert. alert_feedback is
// keyed by (source_type, event_id) (§6.4); the newest row wins should the
// pipeline ever have written more than one.
func feedbackJoin(resolved *scope.DataScope, withFeedback bool) (cols, join string) {
	if !withFeedback {
		return `NULL::text, NULL::boolean, NULL::text, NULL::text, NULL::timestamptz`, ""
	}
	return `f.decision, f.approved_for_baseline, f.reviewer, f.reason, f.updated_at`,
		` LEFT JOIN LATERAL (
		      SELECT fb.decision, fb.approved_for_baseline, fb.reviewer, fb.reason, fb.updated_at
		        FROM ` + resolved.Table("alert_feedback") + ` fb
		       WHERE fb.source_type = a.source_type AND fb.event_id = a.event_id
		       ORDER BY fb.updated_at DESC NULLS LAST
		       LIMIT 1) f ON true `
}

// anomalyWhere renders the filter after the key join; args start at $3.
func anomalyWhere(f AnomalyFilter, withFeedback, withReview bool) (string, []any) {
	var (
		conds []string
		args  []any
	)
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)+2))
	}
	if f.Dimension != "" {
		add("a.dimension = $%d", f.Dimension)
	}
	if f.State != "" {
		add("a.status = $%d", f.State)
	}
	if f.From != nil {
		add("a.created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("a.created_at < $%d", *f.To)
	}
	if withReview && withFeedback {
		switch f.Review {
		case "pending":
			conds = append(conds, "f.decision IS NULL")
		case "reviewed":
			conds = append(conds, "f.decision IS NOT NULL")
		}
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// Counts returns the review counts of every alert of keys matching f
// (f.Review is ignored).
func (r *AnomaliesRepository) Counts(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey, f AnomalyFilter, withFeedback bool) (AnomalyCountsRow, error) {
	var out AnomalyCountsRow
	if len(keys) == 0 {
		return out, nil
	}
	_, join := feedbackJoin(resolved, withFeedback)
	where, args := anomalyWhere(f, withFeedback, false)
	decision := "NULL::text"
	if withFeedback {
		decision = "f.decision"
	}
	q := `SELECT COUNT(*),
	             COUNT(*) FILTER (WHERE ` + decision + ` IS NULL),
	             COUNT(*) FILTER (WHERE ` + decision + ` = 'malicious'),
	             COUNT(*) FILTER (WHERE ` + decision + ` = 'legitimate')
	      FROM ` + resolved.Table("alerts") + ` a` + keyJoin("a") + join + where
	types, names := keyArrays(keys)
	err := resolved.DB.QueryRowContext(ctx, q, append([]any{types, names}, args...)...).
		Scan(&out.Total, &out.Pending, &out.Malicious, &out.Legitimate)
	return out, wrapMonitoringErr("anomaly counts", err)
}

// Page returns up to limit+1 alerts of keys matching f, newest first,
// after the cursor (exclusive).
func (r *AnomaliesRepository) Page(ctx context.Context, resolved *scope.DataScope, keys []MonitoringKey, f AnomalyFilter, withFeedback bool, after *AnomalyCursor, limit int) ([]AlertRow, error) {
	out := []AlertRow{}
	if len(keys) == 0 {
		return out, nil
	}
	cols, join := feedbackJoin(resolved, withFeedback)
	where, args := anomalyWhere(f, withFeedback, true)
	types, names := keyArrays(keys)
	all := append([]any{types, names}, args...)
	if after != nil {
		all = append(all, after.CreatedAt, after.ID)
		cond := fmt.Sprintf("(a.created_at, a.id) < ($%d, $%d)", len(all)-1, len(all))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	all = append(all, limit+1)
	q := `SELECT a.id, a.source_type, a.identity_name, a.dimension, a.reason, a.status, a.created_at,
	             COALESCE(a.event_id, ''), a.observed_value::text, a.baseline_value::text, ` + cols + `
	      FROM ` + resolved.Table("alerts") + ` a` + keyJoin("a") + join + where + fmt.Sprintf(`
	      ORDER BY a.created_at DESC, a.id DESC
	      LIMIT $%d`, len(all))
	rows, err := resolved.DB.QueryContext(ctx, q, all...)
	if err != nil {
		return nil, wrapMonitoringErr("anomaly page", err)
	}
	defer rows.Close()
	for rows.Next() {
		row, err := scanAlertRow(rows)
		if err != nil {
			return nil, wrapMonitoringErr("anomaly page", err)
		}
		out = append(out, row)
	}
	return out, wrapMonitoringErr("anomaly page", rows.Err())
}

type rowScanner interface{ Scan(dest ...any) error }

func scanAlertRow(s rowScanner) (AlertRow, error) {
	var (
		row                        AlertRow
		observed, baseline         sql.NullString
		decision, reviewer, reason sql.NullString
		approved                   sql.NullBool
		updated                    sql.NullTime
	)
	if err := s.Scan(&row.ID, &row.Key.SourceType, &row.Key.IdentityName, &row.Dimension, &row.Reason, &row.Status, &row.CreatedAt,
		&row.EventID, &observed, &baseline, &decision, &approved, &reviewer, &reason, &updated); err != nil {
		return row, err
	}
	row.CreatedAt = row.CreatedAt.UTC()
	row.Observed = shortText(observed)
	row.Baseline = shortText(baseline)
	if decision.Valid {
		fb := &FeedbackRow{Decision: decision.String, ApprovedForBaseline: approved.Bool, Reviewer: reviewer.String}
		if reason.Valid && strings.TrimSpace(reason.String) != "" {
			v := reason.String
			fb.Reason = &v
		}
		if updated.Valid {
			fb.UpdatedAt = updated.Time.UTC()
		}
		row.Feedback = fb
	}
	return row, nil
}

// maxValueText caps baseline/observed values in list responses (§11: no
// large payloads in lists).
const maxValueText = 240

func shortText(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := strings.TrimSpace(v.String)
	if s == "" {
		return nil
	}
	if r := []rune(s); len(r) > maxValueText {
		s = string(r[:maxValueText-1]) + "…"
	}
	return &s
}

// Alert returns one alert of the tenant with its current feedback.
func (r *AnomaliesRepository) Alert(ctx context.Context, resolved *scope.DataScope, id int64, withFeedback bool) (*AlertRow, error) {
	cols, join := feedbackJoin(resolved, withFeedback)
	q := `SELECT a.id, a.source_type, a.identity_name, a.dimension, a.reason, a.status, a.created_at,
	             COALESCE(a.event_id, ''), a.observed_value::text, a.baseline_value::text, ` + cols + `
	      FROM ` + resolved.Table("alerts") + ` a` + join + `
	      WHERE a.id = $1`
	row, err := scanAlertRow(resolved.DB.QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAlertNotFound
	}
	if err != nil {
		return nil, wrapMonitoringErr("anomaly", err)
	}
	return &row, nil
}

// FeedbackWrite is one reviewer decision.
type FeedbackWrite struct {
	Key                 MonitoringKey // the alert's key (for baseline_version)
	EventID             string
	Decision            string
	ApprovedForBaseline bool
	Reviewer            string
	Reason              *string
}

// SaveFeedback writes the one current alert_feedback row of the alert's
// (source_type, event_id): updated when it exists, inserted otherwise, under
// a transaction-scoped advisory lock so two reviewers cannot insert twice.
// baseline_version records the identity's newest published baseline version
// at the time of the decision (NULL when it has none).
func (r *AnomaliesRepository) SaveFeedback(ctx context.Context, resolved *scope.DataScope, w FeedbackWrite) (*FeedbackRow, error) {
	// Read before the transaction: a tenant without baseline_versions only
	// leaves the version empty.
	var version sql.NullInt64
	vq := `SELECT MAX(version) FROM ` + resolved.Table("baseline_versions") + ` WHERE source_type = $1 AND identity_name = $2`
	if err := resolved.DB.QueryRowContext(ctx, vq, w.Key.SourceType, w.Key.IdentityName).Scan(&version); err != nil {
		if err = wrapMonitoringErr("baseline version", err); !IsMonitoringTableMissing(err) {
			return nil, err
		}
		version = sql.NullInt64{}
	}

	tx, err := resolved.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin feedback: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		resolved.Schema+"|alert_feedback|"+w.Key.SourceType+"|"+w.EventID); err != nil {
		return nil, wrapMonitoringErr("lock feedback", err)
	}

	table := resolved.Table("alert_feedback")
	var fb FeedbackRow
	var reason sql.NullString
	update := `UPDATE ` + table + `
	              SET decision = $3, approved_for_baseline = $4, reviewer = $5, reason = $6,
	                  baseline_version = $7, updated_at = now()
	            WHERE source_type = $1 AND event_id = $2
	        RETURNING decision, approved_for_baseline, reviewer, reason, updated_at`
	args := []any{w.Key.SourceType, w.EventID, w.Decision, w.ApprovedForBaseline, w.Reviewer, nullableString(w.Reason), version}
	err = tx.QueryRowContext(ctx, update, args...).Scan(&fb.Decision, &fb.ApprovedForBaseline, &fb.Reviewer, &reason, &fb.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		insert := `INSERT INTO ` + table + ` (source_type, event_id, decision, approved_for_baseline, reviewer, reason, baseline_version, created_at, updated_at)
		           VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		        RETURNING decision, approved_for_baseline, reviewer, reason, updated_at`
		err = tx.QueryRowContext(ctx, insert, args...).Scan(&fb.Decision, &fb.ApprovedForBaseline, &fb.Reviewer, &reason, &fb.UpdatedAt)
	}
	if err != nil {
		return nil, wrapMonitoringErr("save feedback", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit feedback: %w", err)
	}
	if reason.Valid && reason.String != "" {
		v := reason.String
		fb.Reason = &v
	}
	fb.UpdatedAt = fb.UpdatedAt.UTC()
	return &fb, nil
}

func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// AnomalyEventRow is the latest version of one log_events row.
type AnomalyEventRow struct {
	EventTime   time.Time
	EventName   string
	EventSource string
	Region      string
	SourceIP    string
	RiskScore   *int
}

// EventsByID reads the events the alerts were raised on, in one query
// (§6.1 query plan: event_id IN (...), never one query per alert). The
// latest version of each event wins (argMax over ingested_at).
func (e *MonitoringEvents) EventsByID(ctx context.Context, ids []string) (map[string]AnomalyEventRow, error) {
	out := map[string]AnomalyEventRow{}
	if e == nil {
		return out, ErrMonitoringEventsNotConfigured
	}
	if len(ids) == 0 {
		return out, nil
	}
	ctx, cancel := e.queryContext(ctx)
	defer cancel()
	q := `SELECT event_id,
	             argMax(event_time, ingested_at), argMax(event_name, ingested_at), argMax(event_source, ingested_at),
	             argMax(region, ingested_at), argMax(source_ip, ingested_at), argMax(event_risk_score, ingested_at)
	      FROM ` + e.table + `
	      WHERE event_id IN {ids:Array(String)}
	      GROUP BY event_id`
	rows, err := e.conn.Query(ctx, q, ch.Named("ids", ids))
	if err != nil {
		return nil, fmt.Errorf("anomaly events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    string
			row   AnomalyEventRow
			score *uint8
		)
		if err := rows.Scan(&id, &row.EventTime, &row.EventName, &row.EventSource, &row.Region, &row.SourceIP, &score); err != nil {
			return nil, fmt.Errorf("anomaly events: %w", err)
		}
		row.EventTime = row.EventTime.UTC()
		if score != nil {
			v := int(*score)
			row.RiskScore = &v
		}
		out[id] = row
	}
	return out, rows.Err()
}
