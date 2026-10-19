package postures

import (
	"context"
	"fmt"
	"time"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

type IdentityHistoryRepository struct{}

func NewIdentityHistoryRepository() *IdentityHistoryRepository {
	return &IdentityHistoryRepository{}
}

// Get implements GET /identities/{id}/history: the score series. 24h/3d/7d
// read ispm_identity_posture_hourly (one point per scan); 30d/60d/90d read
// ispm_identity_posture_daily (one point per day). The date bounds are plain
// comparisons on the partition key, so only the partitions in the window are
// read.
func (r *IdentityHistoryRepository) Get(ctx context.Context, s *scope.DataScope, id string, w models.Window) ([]models.IdentityHistoryPoint, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	if _, err := loadIdentityRef(ctx, s, id); err != nil {
		return nil, err
	}
	out := make([]models.IdentityHistoryPoint, 0, 64)

	if w.PerScan {
		q := fmt.Sprintf(`SELECT captured_at, score, COALESCE(band, ''), COALESCE(checks_failed, 0)
			FROM %s
			WHERE tenant_id = $1 AND identity_id = $2 AND captured_at >= $3
			ORDER BY captured_at`, s.Table(tPostureHourly))
		rows, err := s.DB.QueryContext(ctx, q, s.TenantID, id, time.Now().Add(-w.Duration))
		if err != nil {
			return nil, fmt.Errorf("query hourly history: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var at time.Time
			var p models.IdentityHistoryPoint
			var score nullableFloat
			if err := rows.Scan(&at, &score, &p.Band, &p.ChecksFailed); err != nil {
				return nil, fmt.Errorf("scan hourly history: %w", err)
			}
			p.At, p.Score = at.UTC().Format(time.RFC3339), score.ptr()
			out = append(out, p)
		}
		return out, rows.Err()
	}

	q := fmt.Sprintf(`SELECT snapshot_date, score, COALESCE(band, ''), COALESCE(checks_failed, 0)
		FROM %s
		WHERE tenant_id = $1 AND identity_id = $2 AND snapshot_date >= $3::date
		ORDER BY snapshot_date`, s.Table(tPostureDaily))
	since := time.Now().UTC().AddDate(0, 0, -w.Days)
	rows, err := s.DB.QueryContext(ctx, q, s.TenantID, id, since)
	if err != nil {
		return nil, fmt.Errorf("query daily history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var day time.Time
		var p models.IdentityHistoryPoint
		var score nullableFloat
		if err := rows.Scan(&day, &score, &p.Band, &p.ChecksFailed); err != nil {
			return nil, fmt.Errorf("scan daily history: %w", err)
		}
		p.At = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		p.Score = score.ptr()
		out = append(out, p)
	}
	return out, rows.Err()
}
