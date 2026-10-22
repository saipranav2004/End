package v1

import (
	"context"
	"database/sql"
	"fmt"
	"nhi-backend-api/internal/logging"
	"runtime/debug"
	"sync"
	"time"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	"nhi-backend-api/internal/scope"
)

// DashboardOverviewRepository implements "ENDPOINT 3: Dashboard Overview"
// (GET /api/v1/dashboard/overview). Postgres tables used: identities,
// credentials, identity_credentials, effective_permissions.
// CloudTrail-derived figures (stale counts, shadow identities, permission
// utilisation) come from the ActivitySource (ClickHouse).
//
// The PDF's detail risk signals define stale as no activity/usage for more
// than 90 days, but do not specify NULL handling. For a missing activity/use
// timestamp, use created_at_aws as the never-used age baseline, then
// first_seen_at if AWS creation time is unavailable. Rows with no usable
// timestamp are not asserted stale. This assumes a missing activity/use date
// means no use was recorded; if NULL instead means telemetry is unavailable,
// the source data needs a separate unknown state. The PDF provides no
// calculation for shadow_identities. The current owner-missing proxy is only
// provisional and must not be presented as an approval-based shadow finding.
//
// Other documented choices:
//   - "permission_utilization_pct": distinct actions CloudTrail saw the
//     credentials use, as a percentage of SUM(effective_permissions.granted_count).
//   - identity_growth / credential_growth: cumulative count by month of
//     first_seen_at, among rows that currently exist (a "when did today's
//     inventory first appear" curve rather than an immutable event log).
type DashboardOverviewRepository struct{}

func NewDashboardOverviewRepository() *DashboardOverviewRepository {
	return &DashboardOverviewRepository{}
}

const staleThresholdDays = 90
const expiringSoonDays = 30
const topOwnersLimit = 10
const expiringSoonLimit = 10

func (r *DashboardOverviewRepository) GetOverview(ctx context.Context, resolved *scope.DataScope, src ActivitySource) (*models.DashboardOverviewResponse, error) {
	resp := &models.DashboardOverviewResponse{
		Charts: models.DashboardCharts{
			IdentitiesByType:            []models.TypeCountFilter{},
			IdentitiesByFunctionalClass: []models.ClassCountFilter{},
			IdentitiesByAccount:         []models.AccountCountItem{},
			IdentitiesByRegion:          []models.RegionFilter{},
			IdentitiesByEnvironment:     []models.EnvironmentCountFilter{},
			IdentitiesByStatus:          []models.StatusCountFilter{},
			IdentityGrowth:              []models.GrowthPoint{},
			CredentialGrowth:            []models.GrowthPoint{},
			CredentialTypes:             []models.TypeCountFilter{},
			TopOwners:                   []models.OwnerCountItem{},
			CredentialsExpiringSoon:     []models.ExpiringCredentialItem{},
			TrustEdgesByType:            []models.EdgeTypeCount{},
			EventVolume:                 []models.GrowthPoint{},
			CredentialUtilization:       []models.BucketCount{},
		},
	}
	if resolved == nil || resolved.DB == nil {
		return nil, errScopeRequired
	}
	src = orNoActivity(src)
	// The four loaders below touch disjoint fields of resp (KPIs, identity
	// charts, credential charts, growth charts) and issue independent queries,
	// so they run concurrently. On the first error the shared context is
	// cancelled, which aborts the queries still in flight instead of leaving
	// them to run to completion on a request that has already failed.
	{
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := runAll(runCtx, cancel,
			func() error { return r.loadKPIs(runCtx, resolved, resp, src) },
			func() error { return r.loadIdentityCharts(runCtx, resolved, resp) },
			func() error { return r.loadCredentialCharts(runCtx, resolved, resp) },
			func() error { return r.loadGrowthCharts(runCtx, resolved, resp) },
			func() error { return r.loadTrustEdges(runCtx, resolved, resp) },
			func() error { r.loadEventVolume(runCtx, resolved, resp, src); return nil },
		); err != nil {
			return nil, err
		}
	}
	resp.ActivityDataAvailable = src.Available() && !src.Degraded()
	return resp, nil
}

// runAll runs independent loaders concurrently and returns the first error,
// cancelling the context so the remaining queries are abandoned rather than
// left to finish for a request that has already failed.
func runAll(ctx context.Context, cancel context.CancelFunc, fns ...func() error) error {
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for _, fn := range fns {
		wg.Add(1)
		go func(fn func() error) {
			defer wg.Done()
			// A panic in a loader goroutine is not covered by net/http's
			// per-request recovery and would crash the whole process.
			defer func() {
				if rec := recover(); rec != nil {
					logging.FromContext(ctx).Error("dashboard loader panic", "panic", rec, "stack", string(debug.Stack()))
					once.Do(func() { firstErr = fmt.Errorf("dashboard loader panic: %v", rec); cancel() })
				}
			}()
			if err := fn(); err != nil {
				once.Do(func() { firstErr = err; cancel() })
			}
		}(fn)
	}
	wg.Wait()
	return firstErr
}

func (r *DashboardOverviewRepository) loadKPIs(ctx context.Context, resolved *scope.DataScope, resp *models.DashboardOverviewResponse, src ActivitySource) error {
	tenantDB, queryArgs := resolved.DB, resolved.QueryArgs()
	now := time.Now()

	// total_identities, publicly_accessible (plain Postgres counts)
	idQuery := fmt.Sprintf(`
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE is_publicly_accessible IS TRUE)::int
		FROM %s.identities i
		WHERE i.account_id = ANY($1::text[]) AND i.region = ANY($2::text[])
		  AND i.deleted_at IS NULL`, resolved.QSchema())

	var totalIdentities, publicCount int
	if err := tenantDB.QueryRowContext(ctx, idQuery, queryArgs...).Scan(&totalIdentities, &publicCount); err != nil {
		return fmt.Errorf("query identity KPIs: %w", err)
	}
	resp.KPIs.TotalIdentities = totalIdentities
	resp.KPIs.PubliclyAccessible = publicCount

	// stale_identities and shadow_identities need CloudTrail: last activity per
	// identity (its ARN, bound credential ARNs, bound access keys) and the
	// creator of owner-less identities.
	idRows, err := r.loadIdentityActivityRows(ctx, tenantDB, resolved.QSchema(), queryArgs)
	if err != nil {
		return err
	}
	staleIdentities, shadowCount := classifyIdentities(ctx, idRows, src, now)
	resp.KPIs.StaleIdentities = staleIdentities
	resp.KPIs.ShadowIdentities = shadowCount
	resp.Charts.StaleVsActive = models.StaleVsActive{Active: totalIdentities - staleIdentities, Stale: staleIdentities}

	// total_credentials, orphaned_credentials (plain Postgres counts)
	credQuery := fmt.Sprintf(`
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (
				WHERE NOT EXISTS (
					SELECT 1 FROM %[1]s.identity_credentials ic
					WHERE ic.credential_id::text = c.id::text
				)
			)::int
		FROM %[1]s.credentials c
		WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
		  AND c.deleted_at IS NULL`, resolved.QSchema())

	var totalCredentials, orphanedCredentials int
	if err := tenantDB.QueryRowContext(ctx, credQuery, queryArgs...).Scan(&totalCredentials, &orphanedCredentials); err != nil {
		return fmt.Errorf("query credential KPIs: %w", err)
	}
	resp.KPIs.TotalCredentials = totalCredentials
	resp.KPIs.OrphanedCredentials = orphanedCredentials

	credRows, err := r.loadCredentialActivityRows(ctx, tenantDB, resolved.QSchema(), queryArgs)
	if err != nil {
		return err
	}
	staleCredentials, usedActions, utilization := classifyCredentials(ctx, credRows, src, now)
	resp.KPIs.StaleCredentials = staleCredentials
	resp.KPIs.ActiveCredentials = totalCredentials - staleCredentials
	resp.Charts.CredentialUtilization = utilization

	// accounts_scanned: distinct accounts across both identities and credentials.
	acctQuery := fmt.Sprintf(`
		SELECT COUNT(DISTINCT account_id) FROM (
			SELECT account_id FROM %[1]s.identities WHERE account_id = ANY($1::text[]) AND region = ANY($2::text[]) AND deleted_at IS NULL AND account_id IS NOT NULL AND account_id != ''
			UNION
			SELECT account_id FROM %[1]s.credentials WHERE account_id = ANY($1::text[]) AND region = ANY($2::text[]) AND deleted_at IS NULL AND account_id IS NOT NULL AND account_id != ''
		) a`, resolved.QSchema())
	var accountsScanned int
	if err := tenantDB.QueryRowContext(ctx, acctQuery, queryArgs...).Scan(&accountsScanned); err != nil {
		return fmt.Errorf("query accounts_scanned: %w", err)
	}
	resp.KPIs.AccountsScanned = accountsScanned

	// permission_utilization_pct: distinct actions used (CloudTrail) as a
	// percentage of SUM(effective_permissions.granted_count).
	permQuery := fmt.Sprintf(`
		SELECT COALESCE(SUM(ep.granted_count), 0)::bigint
		FROM %[1]s.credentials c
		JOIN %[1]s.effective_permissions ep ON ep.credential_id::text = c.id::text
		WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
		  AND c.deleted_at IS NULL`, resolved.QSchema())

	var granted int64
	if err := tenantDB.QueryRowContext(ctx, permQuery, queryArgs...).Scan(&granted); err != nil {
		return fmt.Errorf("query permission_utilization_pct: %w", err)
	}
	if granted > 0 {
		resp.KPIs.PermissionUtilizationPct = round1(float64(usedActions) / float64(granted) * 100)
	}

	return nil
}

// boundKeysCTE yields, per identity, the ARNs of its bound credentials and the
// key IDs of its bound access keys.
const boundKeysCTE = `
	bound_keys AS (
		SELECT ic.identity_id::text AS identity_id,
		       ARRAY_AGG(DISTINCT c.arn) FILTER (WHERE c.arn IS NOT NULL AND c.arn <> '') AS arns,
		       ARRAY_AGG(DISTINCT c.name) FILTER (WHERE LOWER(c.credential_type) = 'access_key' AND c.name IS NOT NULL AND c.name <> '') AS access_keys
		FROM %[1]s.identity_credentials ic
		JOIN %[1]s.credentials c ON c.id::text = ic.credential_id::text AND c.deleted_at IS NULL
		WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
		GROUP BY ic.identity_id
	)`

// identityActivityRow is what stale/shadow classification needs per identity.
type identityActivityRow struct {
	arn        string
	baseline   *time.Time // creation time, used when no activity was ever seen
	ownerBlank bool
	keys       []string // identity ARN, bound credential ARNs, bound access keys
}

func (r *DashboardOverviewRepository) loadIdentityActivityRows(ctx context.Context, tenantDB *sql.DB, schema string, queryArgs []any) ([]identityActivityRow, error) {
	query := fmt.Sprintf(`
		WITH `+boundKeysCTE+`
		SELECT COALESCE(i.arn, ''), COALESCE(i.created_at_aws, i.first_seen_at),
		       (i.owner IS NULL OR BTRIM(i.owner) = ''),
		       COALESCE(bk.arns, '{}'::text[]), COALESCE(bk.access_keys, '{}'::text[])
		FROM %[1]s.identities i
		LEFT JOIN bound_keys bk ON bk.identity_id = i.id::text
		WHERE i.account_id = ANY($1::text[]) AND i.region = ANY($2::text[])
		  AND i.deleted_at IS NULL`, schema)
	rows, err := tenantDB.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query identity activity rows: %w", err)
	}
	defer rows.Close()
	var out []identityActivityRow
	for rows.Next() {
		var row identityActivityRow
		var baseline sql.NullTime
		var arns, keys []string
		if err := rows.Scan(&row.arn, &baseline, &row.ownerBlank, pq.Array(&arns), pq.Array(&keys)); err != nil {
			return nil, fmt.Errorf("scan identity activity row: %w", err)
		}
		if baseline.Valid {
			t := baseline.Time
			row.baseline = &t
		}
		row.keys = append(append([]string{row.arn}, arns...), keys...)
		out = append(out, row)
	}
	return out, rows.Err()
}

// classifyIdentities returns the stale and shadow identity counts.
//
// Stale: no activity for more than 90 days. For an identity with no recorded
// activity the creation time is the never-used baseline (rows with no usable
// timestamp are not asserted stale). Shadow: no owner and, when CloudTrail is
// readable, no known creator either; without CloudTrail the owner-missing
// proxy is used alone.
func classifyIdentities(ctx context.Context, rows []identityActivityRow, src ActivitySource, now time.Time) (stale, shadow int) {
	var allKeys, blankARNs []string
	for _, row := range rows {
		allKeys = append(allKeys, row.keys...)
		if row.ownerBlank {
			blankARNs = append(blankARNs, row.arn)
		}
	}
	var seen map[string]time.Time
	var creators map[string]string
	if src.Available() {
		seen = src.LastSeen(ctx, allKeys)
		creators = src.Creators(ctx, blankARNs)
	}
	cutoff := now.AddDate(0, 0, -staleThresholdDays)
	for _, row := range rows {
		baseline := row.baseline
		if last := latestOf(seen, row.keys); last != nil {
			baseline = last
		}
		if baseline != nil && baseline.Before(cutoff) {
			stale++
		}
		if row.ownerBlank && creators[row.arn] == "" {
			shadow++
		}
	}
	return stale, shadow
}

type credentialActivityRow struct {
	baseline *time.Time
	lastUsed *time.Time // AWS's own last-used date from the scanner
	keys     []string
}

func (r *DashboardOverviewRepository) loadCredentialActivityRows(ctx context.Context, tenantDB *sql.DB, schema string, queryArgs []any) ([]credentialActivityRow, error) {
	query := fmt.Sprintf(`
		SELECT COALESCE(c.arn, ''), COALESCE(c.name, ''), COALESCE(c.credential_type, ''),
		       COALESCE(c.created_at_aws, c.first_seen_at), c.last_used_at
		FROM %s.credentials c
		WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
		  AND c.deleted_at IS NULL`, schema)
	rows, err := tenantDB.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("query credential activity rows: %w", err)
	}
	defer rows.Close()
	var out []credentialActivityRow
	for rows.Next() {
		var arn, name, ctype string
		var baseline, lastUsed sql.NullTime
		if err := rows.Scan(&arn, &name, &ctype, &baseline, &lastUsed); err != nil {
			return nil, fmt.Errorf("scan credential activity row: %w", err)
		}
		row := credentialActivityRow{keys: credentialActivityKeys(arn, name, ctype)}
		if baseline.Valid {
			t := baseline.Time
			row.baseline = &t
		}
		if lastUsed.Valid {
			t := lastUsed.Time
			row.lastUsed = &t
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// utilizationBuckets is the fixed order of charts.credential_utilization.
var utilizationBuckets = []string{"used_7d", "used_30d", "used_90d", "idle_90d", "never_used"}

// utilizationBucket places a credential by its last use: the later of the
// CloudTrail last-seen time and AWS's own last-used date.
func utilizationBucket(lastUse *time.Time, now time.Time) string {
	switch {
	case lastUse == nil:
		return "never_used"
	case lastUse.After(now.AddDate(0, 0, -7)):
		return "used_7d"
	case lastUse.After(now.AddDate(0, 0, -30)):
		return "used_30d"
	case lastUse.After(now.AddDate(0, 0, -90)):
		return "used_90d"
	}
	return "idle_90d"
}

// classifyCredentials returns the stale credential count, the total of
// distinct actions used across all credentials (for permission utilisation)
// and the credentials per last-use bucket.
func classifyCredentials(ctx context.Context, rows []credentialActivityRow, src ActivitySource, now time.Time) (stale, usedActions int, utilization []models.BucketCount) {
	var allKeys []string
	for _, row := range rows {
		allKeys = append(allKeys, row.keys...)
	}
	var seen map[string]time.Time
	var used map[string][]chrepo.UsedAction
	if src.Available() {
		seen = src.LastSeen(ctx, allKeys)
		used = src.UsedActionSets(ctx, allKeys, usageSince(now))
	}
	cutoff := now.AddDate(0, 0, -staleThresholdDays)
	counts := map[string]int{}
	for _, row := range rows {
		baseline := row.baseline
		lastUse := row.lastUsed
		if last := latestOf(seen, row.keys); last != nil {
			baseline = last
			if lastUse == nil || last.After(*lastUse) {
				lastUse = last
			}
		}
		if baseline != nil && baseline.Before(cutoff) {
			stale++
		}
		counts[utilizationBucket(lastUse, now)]++
		usedActions += distinctUsedCount(used, row.keys)
	}
	utilization = make([]models.BucketCount, 0, len(utilizationBuckets))
	for _, b := range utilizationBuckets {
		utilization = append(utilization, models.BucketCount{Bucket: b, Count: counts[b]})
	}
	return stale, usedActions, utilization
}

// loadTrustEdges counts the active trust edges in the selected accounts, in
// total and per edge type. A schema without trust_edges reports none.
func (r *DashboardOverviewRepository) loadTrustEdges(ctx context.Context, resolved *scope.DataScope, resp *models.DashboardOverviewResponse) error {
	q := fmt.Sprintf(`SELECT LOWER(COALESCE(NULLIF(edge_type, ''), 'unknown')), COUNT(*)::int
		FROM %s.trust_edges
		WHERE account_id = ANY($1::text[]) AND COALESCE(is_active, true)
		GROUP BY 1 ORDER BY 2 DESC, 1`, resolved.QSchema())
	rows, err := resolved.DB.QueryContext(ctx, q, resolved.QueryArgs()[0])
	if err != nil {
		if isMissingRelation(err) {
			return nil
		}
		return fmt.Errorf("query trust edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item models.EdgeTypeCount
		if err := rows.Scan(&item.EdgeType, &item.Count); err != nil {
			return fmt.Errorf("scan trust edge count: %w", err)
		}
		resp.Charts.TrustEdgesByType = append(resp.Charts.TrustEdgesByType, item)
		resp.KPIs.TrustEdges += item.Count
		if item.EdgeType == "cross_account" {
			resp.KPIs.CrossAccountTrustEdges += item.Count
		}
	}
	return rows.Err()
}

// eventVolumeDays is the window of charts.event_volume.
const eventVolumeDays = 30

// loadEventVolume fills charts.event_volume from CloudTrail, one point per
// day of the window (zero-filled), or leaves it empty when CloudTrail cannot
// be read. Region selection does not apply: CloudTrail records IAM calls in
// us-east-1, so a region filter would hide them.
func (r *DashboardOverviewRepository) loadEventVolume(ctx context.Context, resolved *scope.DataScope, resp *models.DashboardOverviewResponse, src ActivitySource) {
	if !src.Available() || len(resolved.AccountIDs) == 0 {
		return
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(eventVolumeDays - 1))
	points, ok := src.AccountVolume(ctx, resolved.AccountIDs, start)
	if !ok {
		return
	}
	byDay := make(map[string]int, len(points))
	for _, p := range points {
		byDay[p.Date] = int(p.Count)
	}
	for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
		day := d.Format("2006-01-02")
		resp.Charts.EventVolume = append(resp.Charts.EventVolume, models.GrowthPoint{Date: day, Count: byDay[day]})
	}
}

func (r *DashboardOverviewRepository) loadIdentityCharts(ctx context.Context, resolved *scope.DataScope, resp *models.DashboardOverviewResponse) error {
	tenantDB := resolved.DB
	// identities_by_type
	if err := r.groupCount(ctx, resolved, "identities", "identity_type", func(rows *sql.Rows) error {
		var item models.TypeCountFilter
		if err := rows.Scan(&item.Type, &item.Count); err != nil {
			return err
		}
		resp.Charts.IdentitiesByType = append(resp.Charts.IdentitiesByType, item)
		return nil
	}); err != nil {
		return err
	}

	// identities_by_functional_class
	if err := r.groupCount(ctx, resolved, "identities", "functional_class", func(rows *sql.Rows) error {
		var item models.ClassCountFilter
		if err := rows.Scan(&item.Class, &item.Count); err != nil {
			return err
		}
		resp.Charts.IdentitiesByFunctionalClass = append(resp.Charts.IdentitiesByFunctionalClass, item)
		return nil
	}); err != nil {
		return err
	}

	// identities_by_account
	if err := r.groupCount(ctx, resolved, "identities", "account_id", func(rows *sql.Rows) error {
		var item models.AccountCountItem
		if err := rows.Scan(&item.AccountID, &item.Count); err != nil {
			return err
		}
		resp.Charts.IdentitiesByAccount = append(resp.Charts.IdentitiesByAccount, item)
		return nil
	}); err != nil {
		return err
	}

	// identities_by_region
	if err := r.groupCount(ctx, resolved, "identities", "region", func(rows *sql.Rows) error {
		var item models.RegionFilter
		if err := rows.Scan(&item.Region, &item.Count); err != nil {
			return err
		}
		resp.Charts.IdentitiesByRegion = append(resp.Charts.IdentitiesByRegion, item)
		return nil
	}); err != nil {
		return err
	}

	// identities_by_environment
	if err := r.groupCount(ctx, resolved, "identities", "environment", func(rows *sql.Rows) error {
		var item models.EnvironmentCountFilter
		if err := rows.Scan(&item.Environment, &item.Count); err != nil {
			return err
		}
		resp.Charts.IdentitiesByEnvironment = append(resp.Charts.IdentitiesByEnvironment, item)
		return nil
	}); err != nil {
		return err
	}

	// identities_by_status
	if err := r.groupCount(ctx, resolved, "identities", "status", func(rows *sql.Rows) error {
		var item models.StatusCountFilter
		if err := rows.Scan(&item.Status, &item.Count); err != nil {
			return err
		}
		resp.Charts.IdentitiesByStatus = append(resp.Charts.IdentitiesByStatus, item)
		return nil
	}); err != nil {
		return err
	}

	// top_owners
	ownerQuery := fmt.Sprintf(`
		SELECT owner, COUNT(*)::int
		FROM %s.identities i
		WHERE i.account_id = ANY($1::text[]) AND i.region = ANY($2::text[])
		  AND i.deleted_at IS NULL AND i.owner IS NOT NULL AND BTRIM(i.owner) != ''
		GROUP BY i.owner
		ORDER BY COUNT(*) DESC
		LIMIT %d`, resolved.QSchema(), topOwnersLimit)
	rows, err := tenantDB.QueryContext(ctx, ownerQuery, resolved.QueryArgs()...)
	if err != nil {
		return fmt.Errorf("query top_owners: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item models.OwnerCountItem
		if err := rows.Scan(&item.Owner, &item.Count); err != nil {
			return fmt.Errorf("scan top_owners row: %w", err)
		}
		resp.Charts.TopOwners = append(resp.Charts.TopOwners, item)
	}
	return rows.Err()
}

// groupCount returns an aggregate constrained to the current
// account and region scope and hands each row to scanFn.
func (r *DashboardOverviewRepository) groupCount(ctx context.Context, resolved *scope.DataScope, table, column string, scanFn func(rows *sql.Rows) error) error {
	query := fmt.Sprintf(`
		SELECT x.%[2]s, COUNT(*)::int
		FROM %[3]s.%[1]s x
		WHERE x.account_id = ANY($1::text[]) AND x.region = ANY($2::text[])
		  AND x.deleted_at IS NULL AND x.%[2]s IS NOT NULL AND x.%[2]s != ''
		GROUP BY x.%[2]s ORDER BY COUNT(*) DESC`, table, column, resolved.QSchema())
	rows, err := resolved.DB.QueryContext(ctx, query, resolved.QueryArgs()...)
	if err != nil {
		return fmt.Errorf("query %s grouped by %s: %w", table, column, err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := scanFn(rows); err != nil {
			return fmt.Errorf("scan %s grouped by %s: %w", table, column, err)
		}
	}
	return rows.Err()
}

func (r *DashboardOverviewRepository) loadCredentialCharts(ctx context.Context, resolved *scope.DataScope, resp *models.DashboardOverviewResponse) error {
	tenantDB := resolved.DB
	queryArgs := resolved.QueryArgs()
	// credential_types
	credTypeQuery := fmt.Sprintf(`
		SELECT credential_type, COUNT(*)::int
		FROM %s.credentials c
		WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
		  AND c.deleted_at IS NULL AND c.credential_type IS NOT NULL AND c.credential_type != ''
		GROUP BY c.credential_type
		ORDER BY COUNT(*) DESC`, resolved.QSchema())
	rows, err := tenantDB.QueryContext(ctx, credTypeQuery, queryArgs...)
	if err != nil {
		return fmt.Errorf("query credential_types: %w", err)
	}
	for rows.Next() {
		var item models.TypeCountFilter
		if err := rows.Scan(&item.Type, &item.Count); err != nil {
			rows.Close()
			return fmt.Errorf("scan credential_types row: %w", err)
		}
		resp.Charts.CredentialTypes = append(resp.Charts.CredentialTypes, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// credentials_expiring_soon
	expQuery := fmt.Sprintf(`
		SELECT id::text, COALESCE(name, arn, id::text), expires_at,
		       GREATEST(CEIL(EXTRACT(EPOCH FROM (expires_at - NOW())) / 86400), 0)::int
		FROM %s.credentials c
		WHERE c.account_id = ANY($1::text[]) AND c.region = ANY($2::text[])
		  AND c.deleted_at IS NULL AND c.expires_at IS NOT NULL
		  AND c.expires_at BETWEEN NOW() AND NOW() + INTERVAL '%d days'
		ORDER BY c.expires_at ASC
		LIMIT %d`, resolved.QSchema(), expiringSoonDays, expiringSoonLimit)
	expRows, err := tenantDB.QueryContext(ctx, expQuery, queryArgs...)
	if err != nil {
		return fmt.Errorf("query credentials_expiring_soon: %w", err)
	}
	defer expRows.Close()
	for expRows.Next() {
		var item models.ExpiringCredentialItem
		if err := expRows.Scan(&item.CredentialID, &item.CredentialName, &item.ExpiresAt, &item.DaysRemaining); err != nil {
			return fmt.Errorf("scan credentials_expiring_soon row: %w", err)
		}
		resp.Charts.CredentialsExpiringSoon = append(resp.Charts.CredentialsExpiringSoon, item)
	}
	return expRows.Err()
}

// loadGrowthCharts builds a cumulative "growth" curve bucketed by month,
// using first_seen_at among rows that currently exist. This reflects "when
// did today's inventory first appear" rather than a true historical event log.
func (r *DashboardOverviewRepository) loadGrowthCharts(ctx context.Context, resolved *scope.DataScope, resp *models.DashboardOverviewResponse) error {
	idGrowth, err := r.monthlyCumulativeGrowth(ctx, resolved, "identities")
	if err != nil {
		return err
	}
	resp.Charts.IdentityGrowth = idGrowth

	credGrowth, err := r.monthlyCumulativeGrowth(ctx, resolved, "credentials")
	if err != nil {
		return err
	}
	resp.Charts.CredentialGrowth = credGrowth

	return nil
}

func (r *DashboardOverviewRepository) monthlyCumulativeGrowth(ctx context.Context, resolved *scope.DataScope, table string) ([]models.GrowthPoint, error) {
	query := fmt.Sprintf(`
		SELECT DATE_TRUNC('month', first_seen_at) AS month, COUNT(*)::int
		FROM %s.%s x
		WHERE x.account_id = ANY($1::text[]) AND x.region = ANY($2::text[])
		  AND x.deleted_at IS NULL AND x.first_seen_at IS NOT NULL
		GROUP BY month ORDER BY month`, resolved.QSchema(), table)

	rows, err := resolved.DB.QueryContext(ctx, query, resolved.QueryArgs()...)
	if err != nil {
		return nil, fmt.Errorf("query %s growth: %w", table, err)
	}
	defer rows.Close()

	points := make([]models.GrowthPoint, 0)
	running := 0
	for rows.Next() {
		var month time.Time
		var cnt int
		if err := rows.Scan(&month, &cnt); err != nil {
			return nil, fmt.Errorf("scan %s growth row: %w", table, err)
		}
		running += cnt
		points = append(points, models.GrowthPoint{Date: month.Format("2006-01-02"), Count: running})
	}
	return points, rows.Err()
}

// Note: round1() is already defined in credential_detail_repo.go (shared
// within this package).

// effectiveIdleDays is how long something has gone unused: days since its
// last recorded use, or, when it was never used, days since it was created -
// the rule the stale counts above apply, so a list filtered on idle days and
// the dashboard's stale counts agree.
func effectiveIdleDays(idleDays, ageDays *int) *int {
	if idleDays != nil {
		return idleDays
	}
	return ageDays
}
