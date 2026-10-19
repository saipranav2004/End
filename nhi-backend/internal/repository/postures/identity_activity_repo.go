package postures

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/postures"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

// IdentityActivityRepository reads the identity's CloudTrail calls from the
// scanner's event store (ClickHouse). When that store is not configured or
// cannot be reached the endpoint answers "no data" instead of failing.
type IdentityActivityRepository struct {
	ch     *chrepo.Repository
	lookup *repov1.IdentityLookupRepository
}

func NewIdentityActivityRepository(ch *chrepo.Repository, lookup *repov1.IdentityLookupRepository) *IdentityActivityRepository {
	return &IdentityActivityRepository{ch: ch, lookup: lookup}
}

type activityCursor struct {
	T  time.Time `json:"t"`
	ID string    `json:"id"`
}

// Get implements GET /identities/{id}/activity. It returns the events, the
// next cursor, and whether the event store could be read.
func (r *IdentityActivityRepository) Get(ctx context.Context, s *scope.DataScope, id string, f models.IdentityActivityFilter) ([]models.ActivityEvent, *string, bool, error) {
	if err := need(s); err != nil {
		return nil, nil, false, err
	}
	ref, err := loadIdentityRef(ctx, s, id)
	if err != nil {
		return nil, nil, false, err
	}
	empty := []models.ActivityEvent{}
	if !r.ch.Enabled() {
		return empty, nil, false, nil
	}
	limit := clampLimit(f.Limit)

	pf := chrepo.ActivityPageFilter{Mutating: f.Mutating, Limit: limit + 1}
	if strings.TrimSpace(f.Cursor) != "" {
		var c activityCursor
		if err := decodeCursor(f.Cursor, &c); err != nil || c.ID == "" || c.T.IsZero() {
			return nil, nil, false, ErrInvalidCursor
		}
		pf.HasCursor, pf.CursorTime, pf.CursorID = true, c.T, c.ID
	}

	keys, err := r.subjectKeys(ctx, s, ref)
	if err != nil {
		return nil, nil, false, err
	}
	accounts, err := r.lookup.TenantAccountIDs(ctx, s.DB, s.QSchema(), s.TenantID)
	if err != nil {
		return nil, nil, false, err
	}
	sc := chrepo.Scope{
		Accounts: accounts,
		Start:    time.Now().UTC().Add(-f.Window.Duration),
		Service:  chrepo.ServiceName(f.Service),
	}
	events, err := r.ch.ActivityPage(ctx, chrepo.NewSubject(keys...), sc, pf)
	if err != nil {
		msg := err.Error()
		if len(msg) > 300 { // ClickHouse errors echo the whole query
			msg = msg[:300] + "..."
		}
		log.Printf("clickhouse identity activity: %s (activity data unavailable)", msg)
		return empty, nil, false, nil
	}

	var next *string
	if len(events) > limit {
		events = events[:limit]
		last := events[limit-1]
		c := encodeCursor(activityCursor{T: last.EventTime.UTC(), ID: last.EventID})
		next = &c
	}
	out := make([]models.ActivityEvent, 0, len(events))
	for _, e := range events {
		ev := models.ActivityEvent{
			EventTime: e.EventTime.UTC(), Action: e.EventName, Service: e.EventSource,
			SourceIP: e.SourceIP, Region: e.Region, Mutating: !e.IsReadOnly,
		}
		if e.ErrorCode != "" {
			code := e.ErrorCode
			ev.ErrorCode = &code
		}
		out = append(out, ev)
	}
	return out, next, true, nil
}

// subjectKeys are the CloudTrail identifiers of the identity: its own ARN, the
// credentials it is bound to, and the access-key ids inside those ARNs.
func (r *IdentityActivityRepository) subjectKeys(ctx context.Context, s *scope.DataScope, ref *identityRef) ([]string, error) {
	keys := []string{ref.ARN}
	if ref.ARN == "" {
		return keys, nil
	}
	q := fmt.Sprintf(`SELECT DISTINCT target_arn FROM %s
		WHERE tenant_id = $1 AND source_arn = $2 AND edge_type = 'BOUND_TO'`, s.Table(tAccessEdges))
	rows, err := s.DB.QueryContext(ctx, q, s.TenantID, ref.ARN)
	if err != nil {
		if isUndefinedTable(err) {
			return keys, nil
		}
		return nil, fmt.Errorf("load activity keys: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var arn string
		if err := rows.Scan(&arn); err != nil {
			return nil, fmt.Errorf("scan activity key: %w", err)
		}
		keys = append(keys, arn)
		if id, ok := accessKeyID(arn); ok {
			keys = append(keys, id)
		}
	}
	return keys, rows.Err()
}
