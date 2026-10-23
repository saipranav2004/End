package v1

import (
	"context"
	"fmt"
	"time"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/scope"
)

// DashboardTrendsRepository reads identity and credential totals across the
// completed scans (<schema>.nhi_scans_aws.total_identities / total_credentials).
type DashboardTrendsRepository struct{}

func NewDashboardTrendsRepository() *DashboardTrendsRepository { return &DashboardTrendsRepository{} }

// Accounts narrows the caller's scope to the requested accounts / regions:
// only accounts of this tenant (its own schema) count, and an account matches
// a region filter when it reads one of those regions. nil lists keep the
// scope's own accounts.
func (r *DashboardTrendsRepository) Accounts(ctx context.Context, sc *scope.DataScope, accountIDs, regions []string) ([]string, error) {
	if len(accountIDs) == 0 && len(regions) == 0 {
		return sc.AccountIDs, nil
	}
	from := accountIDs
	if len(from) == 0 {
		from = sc.AccountIDs
	}
	q := fmt.Sprintf(`SELECT account_id FROM %s WHERE account_id = ANY($1::text[])`, sc.Table("accounts"))
	args := []any{pq.Array(from)}
	if len(regions) > 0 {
		q += ` AND COALESCE(regions, '{}'::text[]) && $2::text[]`
		args = append(args, pq.Array(regions))
	}
	rows, err := sc.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("trend accounts: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Trends returns the newest `limit` points, oldest first.
//
// aggregate=false: one row per completed scan (scan_id, account_id).
// aggregate=true: one row per hour in which any selected account finished a
// scan; each account contributes the totals of its latest scan up to that
// hour, so a scan cycle that reaches the accounts one after another does not
// read as a dip while the others are still waiting.
func (r *DashboardTrendsRepository) Trends(ctx context.Context, sc *scope.DataScope, accounts []string, limit int, aggregate bool) ([]models.DashboardTrendPoint, error) {
	if len(accounts) == 0 {
		return []models.DashboardTrendPoint{}, nil
	}
	scans := sc.Table("nhi_scans_aws")
	var q string
	if aggregate {
		q = fmt.Sprintf(`
WITH s AS (
  SELECT account_id, date_trunc('hour', scan_end) AS bucket, scan_end,
         COALESCE(total_identities, 0) AS ids, COALESCE(total_credentials, 0) AS creds
  FROM %s
  WHERE upper(status) = 'COMPLETED' AND scan_end IS NOT NULL AND account_id = ANY($1::text[])
), latest AS (
  SELECT DISTINCT ON (account_id, bucket) account_id, bucket, ids, creds FROM s ORDER BY account_id, bucket, scan_end DESC
), buckets AS (SELECT DISTINCT bucket FROM latest ORDER BY bucket DESC LIMIT $2)
SELECT b.bucket, '' AS scan_id, '' AS account_id, SUM(x.ids)::int, SUM(x.creds)::int, COUNT(*)::int
FROM buckets b
CROSS JOIN LATERAL (
  SELECT DISTINCT ON (l.account_id) l.ids, l.creds FROM latest l WHERE l.bucket <= b.bucket ORDER BY l.account_id, l.bucket DESC
) x
GROUP BY b.bucket ORDER BY b.bucket ASC`, scans)
	} else {
		q = fmt.Sprintf(`
SELECT scan_date, scan_id, account_id, ids, creds, 1 FROM (
  SELECT scan_end AS scan_date, scan_id::text AS scan_id, account_id, COALESCE(total_identities, 0) AS ids, COALESCE(total_credentials, 0) AS creds
  FROM %s
  WHERE upper(status) = 'COMPLETED' AND scan_end IS NOT NULL AND account_id = ANY($1::text[])
  ORDER BY scan_end DESC, id DESC LIMIT $2
) t ORDER BY scan_date ASC`, scans)
	}
	rows, err := sc.DB.QueryContext(ctx, q, pq.Array(accounts), limit)
	if err != nil {
		if isMissingRelation(err) {
			return []models.DashboardTrendPoint{}, nil // no scans yet
		}
		return nil, fmt.Errorf("dashboard trends: %w", err)
	}
	defer rows.Close()
	out := []models.DashboardTrendPoint{}
	for rows.Next() {
		var p models.DashboardTrendPoint
		var at time.Time
		if err := rows.Scan(&at, &p.ScanID, &p.AccountID, &p.TotalIdentities, &p.TotalCredentials, &p.Accounts); err != nil {
			return nil, err
		}
		p.ScanDate = at.UTC().Format(time.RFC3339)
		out = append(out, p)
	}
	return out, rows.Err()
}
