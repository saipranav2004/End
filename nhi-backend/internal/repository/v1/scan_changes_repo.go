package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/scope"
)

// ═══════════════════════════════════════════════════════════════════════════
// Scan change tracking, read from the discovery scanner's own tables:
//   <schema>.nhi_scans_aws   one row per run (scan_id text, account_id,
//                            status 'COMPLETED', scan_end)
//   <schema>.scan_changes    one row per difference (id uuid, scan_id,
//                            previous_scan_id, change_type, entity_type,
//                            entity_arn, entity_id, summary, details, detected_at)
//
// A scan's previous scan is the account's preceding completed run (so a run
// that found nothing still points at its predecessor). scan_changes has no
// account or region column: both come from the entity ARN (IAM ARNs carry
// no region and are 'global', as in identities), the account falling back to
// the scan's.
//
// Scope, as everywhere in the inventory API:
//   - a scan is visible when it completed for an account in the selection;
//   - a change is visible when its account and region are in the selection.
//
// When the tables do not exist every read answers "no scans yet" rather than
// failing: change tracking is optional for a workspace.
// ═══════════════════════════════════════════════════════════════════════════

// ErrScanNotFound: no completed scan with that id is visible to the caller.
var ErrScanNotFound = errors.New("scan not found")

// LatestScanID is the alias GET /scans/latest/changes accepts.
const LatestScanID = "latest"

type ScanChangesRepository struct{}

func NewScanChangesRepository() *ScanChangesRepository { return &ScanChangesRepository{} }

// isMissingRelation reports undefined_table (42P01): the migration has not run.
func isMissingRelation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		return true
	}
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "42P01"
}

// completedScans is every visible completed run with its predecessor; $1 is
// the caller's accounts. nhi_scans_aws has no start time; scan_end stands in.
func completedScans(sc *scope.DataScope) string {
	return fmt.Sprintf(`SELECT s.id, s.scan_id::text AS scan_id, s.account_id::text AS account_id, s.scan_end,
		       LAG(s.scan_id::text) OVER w AS previous_scan_id, LAG(s.scan_end) OVER w AS previous_at
		FROM %s s
		WHERE upper(s.status) = 'COMPLETED' AND s.scan_end IS NOT NULL
		  AND s.account_id = ANY($1::text[])
		WINDOW w AS (PARTITION BY s.account_id ORDER BY s.scan_end, s.id)`, sc.Table("nhi_scans_aws"))
}

// scanSelect reads visible scans in ScanInfo column order.
func scanSelect(sc *scope.DataScope) string {
	return `SELECT s.scan_id, 'completed', s.account_id, s.scan_end, s.scan_end, s.previous_scan_id, s.previous_at
		FROM (` + completedScans(sc) + `) s WHERE true`
}

// scanOrder is newest first.
const scanOrder = ` ORDER BY s.scan_end DESC, s.id DESC`

func scanScan(row interface{ Scan(...any) error }) (*models.ScanInfo, error) {
	var info models.ScanInfo
	var account, previous sql.NullString
	var completed, previousAt sql.NullTime
	if err := row.Scan(&info.ScanID, &info.Status, &account, &info.StartedAt, &completed, &previous, &previousAt); err != nil {
		return nil, err
	}
	if account.Valid {
		info.AccountID = &account.String
	}
	if completed.Valid {
		t := completed.Time.UTC()
		info.CompletedAt = &t
	}
	if previous.Valid && previous.String != "" {
		info.PreviousScanID = &previous.String
	}
	if previousAt.Valid {
		t := previousAt.Time.UTC()
		info.PreviousScanAt = &t
	}
	info.StartedAt = info.StartedAt.UTC()
	return &info, nil
}

func requireScope(sc *scope.DataScope) error {
	if sc == nil || sc.DB == nil {
		return errScopeRequired
	}
	return nil
}

// LatestScan returns the newest visible completed scan, or nil when there is
// none (or change tracking is not set up).
func (r *ScanChangesRepository) LatestScan(ctx context.Context, sc *scope.DataScope) (*models.ScanInfo, error) {
	if err := requireScope(sc); err != nil {
		return nil, err
	}
	q := scanSelect(sc) + scanOrder + ` LIMIT 1`
	info, err := scanScan(sc.DB.QueryRowContext(ctx, q, pq.Array(sc.AccountIDs)))
	switch {
	case errors.Is(err, sql.ErrNoRows), err != nil && isMissingRelation(err):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("load latest scan: %w", err)
	}
	return info, nil
}

// Scan returns one visible completed scan; scanID may be "latest".
func (r *ScanChangesRepository) Scan(ctx context.Context, sc *scope.DataScope, scanID string) (*models.ScanInfo, error) {
	if scanID == LatestScanID {
		info, err := r.LatestScan(ctx, sc)
		if err != nil {
			return nil, err
		}
		if info == nil {
			return nil, ErrScanNotFound
		}
		return info, nil
	}
	if err := requireScope(sc); err != nil {
		return nil, err
	}
	q := scanSelect(sc) + ` AND s.scan_id = $2`
	info, err := scanScan(sc.DB.QueryRowContext(ctx, q, pq.Array(sc.AccountIDs), scanID))
	switch {
	case errors.Is(err, sql.ErrNoRows), err != nil && isMissingRelation(err):
		return nil, ErrScanNotFound
	case err != nil:
		return nil, fmt.Errorf("load scan: %w", err)
	}
	return info, nil
}

// ListScans returns the newest visible completed scans with their in-scope
// change counts.
func (r *ScanChangesRepository) ListScans(ctx context.Context, sc *scope.DataScope, limit int) ([]models.ScanListItem, error) {
	if err := requireScope(sc); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT x.scan_id, x.status, x.account_id, x.started_at, x.completed_at, x.previous_scan_id, x.previous_at,
		       (SELECT count(*) FROM %[1]s c
		         WHERE c.scan_id = x.scan_id AND %[3]s)
		FROM (%[2]s%[4]s LIMIT $3) AS x (scan_id, status, account_id, started_at, completed_at, previous_scan_id, previous_at)
		ORDER BY x.completed_at DESC`, sc.Table("scan_changes"), scanSelect(sc), changeScopeFor("x.account_id"), scanOrder)
	rows, err := sc.DB.QueryContext(ctx, q, pq.Array(sc.AccountIDs), pq.Array(sc.Regions), limit)
	if err != nil {
		if isMissingRelation(err) {
			return []models.ScanListItem{}, nil
		}
		return nil, fmt.Errorf("list scans: %w", err)
	}
	defer rows.Close()
	out := []models.ScanListItem{}
	for rows.Next() {
		var total int
		var info *models.ScanInfo
		info, err = scanScan(scanRow{rows: rows, extra: []any{&total}})
		if err != nil {
			return nil, fmt.Errorf("scan scans row: %w", err)
		}
		out = append(out, models.ScanListItem{ScanInfo: *info, TotalChanges: total})
	}
	return out, rows.Err()
}

// scanRow lets scanScan read a row that carries extra trailing columns.
type scanRow struct {
	rows  *sql.Rows
	extra []any
}

func (s scanRow) Scan(dest ...any) error { return s.rows.Scan(append(dest, s.extra...)...) }

// changeAccount and changeRegion read a change's account and region from its
// ARN (arn:partition:service:region:account:resource).
const (
	changeRegion     = `COALESCE(NULLIF(split_part(c.entity_arn, ':', 4), ''), 'global')`
	changeAccountARN = `NULLIF(split_part(c.entity_arn, ':', 5), '')`
)

// changeScopeFor is the change-level scope predicate ($1 accounts, $2
// regions); scanAccount is the SQL for the change's scan account.
func changeScopeFor(scanAccount string) string {
	return `COALESCE(` + changeAccountARN + `, ` + scanAccount + `) = ANY($1::text[]) AND ` + changeRegion + ` = ANY($2::text[]) AND ` + notScannerNoise
}

// notScannerNoise drops a modification whose only difference is the
// scanner's own scan_errors inside type_attributes: AWS error text carries a
// fresh RequestID on every run, so without this every identity reads as
// changed after every scan. Nested CASEs, because SQL does not promise to
// evaluate AND in order and the casts must only run on valid JSON objects.
// pg_input_is_valid needs PostgreSQL 16+.
const notScannerNoise = `NOT COALESCE(CASE WHEN upper(c.change_type) = 'MODIFIED'
	  AND jsonb_typeof(c.details->'fields_changed') = 'array' THEN
	    CASE WHEN jsonb_array_length(c.details->'fields_changed') = 1
	      AND c.details->'fields_changed'->0->>'field' = 'type_attributes'
	      AND pg_input_is_valid(c.details->'fields_changed'->0->>'old_value', 'jsonb')
	      AND pg_input_is_valid(c.details->'fields_changed'->0->>'new_value', 'jsonb') THEN
	        CASE WHEN jsonb_typeof((c.details->'fields_changed'->0->>'old_value')::jsonb) = 'object'
	          AND jsonb_typeof((c.details->'fields_changed'->0->>'new_value')::jsonb) = 'object' THEN
	            ((c.details->'fields_changed'->0->>'old_value')::jsonb - 'scan_errors') =
	            ((c.details->'fields_changed'->0->>'new_value')::jsonb - 'scan_errors')
	        END
	    END
	END, false)`

// changeScope is changeScopeFor joined to the scan as s.
var changeScope = changeScopeFor("s.account_id")

// Summary counts a scan's in-scope changes by change type and entity type.
func (r *ScanChangesRepository) Summary(ctx context.Context, sc *scope.DataScope, scanID string) (models.ScanChangeSummary, error) {
	if err := requireScope(sc); err != nil {
		return emptySummary(), err
	}
	summary, _, err := r.countsByPair(ctx, sc, scanID)
	return summary, err
}

func emptySummary() models.ScanChangeSummary {
	s := models.ScanChangeSummary{ByChangeType: map[string]int{}, ByEntityType: map[string]int{}}
	for _, t := range models.ChangeTypes {
		s.ByChangeType[t] = 0
	}
	for _, t := range models.EntityTypes {
		s.ByEntityType[t] = 0
	}
	return s
}

// changeColumns reads one change in scanChange order. The name comes from
// the inventory row (by entity_id, else ARN); a deleted entity may have none.
func changeColumns(sc *scope.DataScope) string {
	return fmt.Sprintf(`c.id::text, c.scan_id::text, s.scan_end, upper(c.change_type), lower(c.entity_type),
	c.entity_arn,
	COALESCE(CASE lower(c.entity_type)
		WHEN 'credential' THEN (SELECT cr.name FROM %[2]s cr WHERE cr.id = c.entity_id OR cr.arn = c.entity_arn ORDER BY (cr.id = c.entity_id) DESC LIMIT 1)
		ELSE (SELECT i.name FROM %[1]s i WHERE i.id = c.entity_id OR i.arn = c.entity_arn ORDER BY (i.id = c.entity_id) DESC LIMIT 1)
	END, ''),
	CASE WHEN lower(c.entity_type) <> 'credential' THEN c.entity_id::text END,
	CASE WHEN lower(c.entity_type) = 'credential' THEN c.entity_id::text END,
	COALESCE(%[3]s, s.account_id, ''), %[4]s,
	COALESCE(c.summary, ''), c.details, c.detected_at`, sc.Table("identities"), sc.Table("credentials"), changeAccountARN, changeRegion)
}

// changeOrder lists a scan's changes the way a reviewer reads them: who and
// what first (identities, credentials, trust), then the permission detail.
const changeOrder = `CASE lower(c.entity_type) WHEN 'identity' THEN 0 WHEN 'credential' THEN 1 WHEN 'trust_edge' THEN 2
	WHEN 'permission' THEN 3 ELSE 4 END,
	CASE upper(c.change_type) WHEN 'CREATED' THEN 0 WHEN 'DELETED' THEN 1 ELSE 2 END,
	c.entity_arn, c.id`

// changesFrom joins changes to their visible scans ($1 = accounts).
func changesFrom(sc *scope.DataScope) string {
	return fmt.Sprintf(`%s c JOIN (%s) s ON s.scan_id = c.scan_id`, sc.Table("scan_changes"), completedScans(sc))
}

func scanChange(rows *sql.Rows) (models.ScanChange, error) {
	var ch models.ScanChange
	var scanAt sql.NullTime
	var identityID, credentialID sql.NullString
	var details []byte
	if err := rows.Scan(&ch.ChangeID, &ch.ScanID, &scanAt, &ch.ChangeType, &ch.EntityType, &ch.EntityARN, &ch.EntityName,
		&identityID, &credentialID, &ch.AccountID, &ch.Region, &ch.Summary, &details, &ch.DetectedAt); err != nil {
		return ch, err
	}
	if scanAt.Valid {
		t := scanAt.Time.UTC()
		ch.ScanAt = &t
	}
	if identityID.Valid {
		ch.IdentityID = &identityID.String
	}
	if credentialID.Valid {
		ch.CredentialID = &credentialID.String
	}
	if len(details) == 0 || !json.Valid(details) {
		details = []byte("{}")
	}
	ch.Details = json.RawMessage(details)
	ch.DetectedAt = ch.DetectedAt.UTC()
	if strings.TrimSpace(ch.Summary) == "" {
		ch.Summary = DescribeChange(ch.ChangeType, ch.EntityType, ch.EntityName, ch.EntityARN)
	}
	return ch, nil
}

// Changes returns one page of a scan's in-scope changes and the filtered total.
func (r *ScanChangesRepository) Changes(ctx context.Context, sc *scope.DataScope, f models.ScanChangesFilter) ([]models.ScanChange, int, error) {
	if err := requireScope(sc); err != nil {
		return nil, 0, err
	}
	args := []any{pq.Array(sc.AccountIDs), pq.Array(sc.Regions), f.ScanID}
	where := "c.scan_id = $3 AND " + changeScope
	if f.ChangeType != "" {
		args = append(args, f.ChangeType)
		where += fmt.Sprintf(" AND upper(c.change_type) = $%d", len(args))
	}
	if f.EntityType != "" {
		args = append(args, f.EntityType)
		where += fmt.Sprintf(" AND lower(c.entity_type) = $%d", len(args))
	}
	from := changesFrom(sc)

	var total int
	if err := sc.DB.QueryRowContext(ctx, `SELECT count(*) FROM `+from+` WHERE `+where, args...).Scan(&total); err != nil {
		if isMissingRelation(err) {
			return []models.ScanChange{}, 0, nil
		}
		return nil, 0, fmt.Errorf("count scan changes: %w", err)
	}
	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	q := fmt.Sprintf(`SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
		changeColumns(sc), from, where, changeOrder, len(args)-1, len(args))
	rows, err := sc.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list scan changes: %w", err)
	}
	defer rows.Close()
	out := []models.ScanChange{}
	for rows.Next() {
		ch, err := scanChange(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan change row: %w", err)
		}
		out = append(out, ch)
	}
	return out, total, rows.Err()
}

// EntityChanges returns the changes of one identity (column identity_id) or
// credential (credential_id), matched by entity_id or ARN, newest first,
// across every visible completed scan. available is false when the tables do
// not exist.
func (r *ScanChangesRepository) EntityChanges(ctx context.Context, sc *scope.DataScope, column, id, arn string, limit int) (changes []models.ScanChange, truncated, available bool, err error) {
	if err := requireScope(sc); err != nil {
		return nil, false, false, err
	}
	if column != "identity_id" && column != "credential_id" {
		return nil, false, false, fmt.Errorf("entity changes: unsupported column %q", column)
	}
	// entity_id is shared by both kinds; the kind keeps an identity's tab
	// from matching a credential that happens to share its ARN.
	kind := "lower(c.entity_type) <> 'credential'"
	if column == "credential_id" {
		kind = "lower(c.entity_type) = 'credential'"
	}
	var idArg any
	if uuidLike(id) {
		idArg = id
	}
	q := fmt.Sprintf(`SELECT %s FROM %s
		WHERE %s AND %s AND (c.entity_id = $3::uuid OR c.entity_arn = $4)
		ORDER BY s.scan_end DESC, c.detected_at DESC, c.id DESC
		LIMIT $5`, changeColumns(sc), changesFrom(sc), changeScope, kind)
	rows, err := sc.DB.QueryContext(ctx, q, pq.Array(sc.AccountIDs), pq.Array(sc.Regions), idArg, arn, limit+1)
	if err != nil {
		if isMissingRelation(err) {
			return []models.ScanChange{}, false, false, nil
		}
		return nil, false, false, fmt.Errorf("list entity changes: %w", err)
	}
	defer rows.Close()
	out := []models.ScanChange{}
	for rows.Next() {
		ch, err := scanChange(rows)
		if err != nil {
			return nil, false, true, fmt.Errorf("scan entity change row: %w", err)
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, false, true, err
	}
	if len(out) > limit {
		return out[:limit], true, true, nil
	}
	return out, false, true, nil
}

// RecentChangeFlags answers, for list rows, what the latest visible scan did
// to each: "CREATED" when it created the row's own entity, "MODIFIED" when it
// changed anything belonging to it (its permissions, bindings or trust). The
// map is keyed by the row id and by its ARN. kind is "identity" or
// "credential".
func (r *ScanChangesRepository) RecentChangeFlags(ctx context.Context, sc *scope.DataScope, kind string, ids, arns []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 && len(arns) == 0 {
		return out, nil
	}
	latest, err := r.LatestScan(ctx, sc)
	if err != nil || latest == nil {
		return out, err
	}
	q := fmt.Sprintf(`SELECT COALESCE(c.entity_id::text, ''), c.entity_arn, lower(c.entity_type), upper(c.change_type)
		FROM %[1]s c
		WHERE c.scan_id = $3 AND %[2]s
		  AND (c.entity_id = ANY($4::uuid[]) OR c.entity_arn = ANY($5::text[]))`, sc.Table("scan_changes"), changeScopeFor("$6::text"))
	validIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		if uuidLike(id) {
			validIDs = append(validIDs, id)
		}
	}
	rows, err := sc.DB.QueryContext(ctx, q, pq.Array(sc.AccountIDs), pq.Array(sc.Regions), latest.ScanID, pq.Array(validIDs), pq.Array(arns), scanAccountID(latest))
	if err != nil {
		if isMissingRelation(err) {
			return out, nil
		}
		return nil, fmt.Errorf("recent change flags: %w", err)
	}
	defer rows.Close()
	mark := func(key, flag string) {
		if key == "" {
			return
		}
		if flag == models.ChangeCreated || out[key] == "" {
			out[key] = flag
		}
	}
	for rows.Next() {
		var id, arn, entityType, changeType string
		if err := rows.Scan(&id, &arn, &entityType, &changeType); err != nil {
			return nil, fmt.Errorf("scan change flag: %w", err)
		}
		flag := models.ChangeModified
		switch {
		case entityType == kind && changeType == models.ChangeCreated:
			flag = models.ChangeCreated
		case entityType == kind && changeType == models.ChangeDeleted:
			continue // a deleted row is not in the list
		}
		// entity_id belongs to the change's own entity type.
		if entityType == kind {
			mark(id, flag)
			mark(arn, flag)
		}
	}
	return out, rows.Err()
}

func scanAccountID(s *models.ScanInfo) string {
	if s == nil || s.AccountID == nil {
		return ""
	}
	return *s.AccountID
}

func uuidLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
				return false
			}
		}
	}
	return true
}

// RecentChanges builds the dashboard's "Recent changes" for the latest
// visible scan, or nil before the first scan.
func (r *ScanChangesRepository) RecentChanges(ctx context.Context, sc *scope.DataScope) (*models.RecentChanges, error) {
	latest, err := r.LatestScan(ctx, sc)
	if err != nil || latest == nil {
		return nil, err
	}
	summary, counts, err := r.countsByPair(ctx, sc, latest.ScanID)
	if err != nil {
		return nil, err
	}
	at := latest.CompletedAt
	out := &models.RecentChanges{
		LastScanID:     latest.ScanID,
		LastScanAt:     at,
		PreviousScanID: latest.PreviousScanID,
		PreviousScanAt: latest.PreviousScanAt,
		TotalChanges:   summary.Total,
		ByChangeType:   summary.ByChangeType,
		Highlights:     Highlights(counts),
	}
	return out, nil
}

type changePair struct{ changeType, entityType string }

func (r *ScanChangesRepository) countsByPair(ctx context.Context, sc *scope.DataScope, scanID string) (models.ScanChangeSummary, map[changePair]int, error) {
	summary := emptySummary()
	counts := map[changePair]int{}
	q := fmt.Sprintf(`SELECT upper(c.change_type), lower(c.entity_type), count(*)
		FROM %s WHERE c.scan_id = $3 AND %s
		GROUP BY 1, 2`, changesFrom(sc), changeScope)
	rows, err := sc.DB.QueryContext(ctx, q, pq.Array(sc.AccountIDs), pq.Array(sc.Regions), scanID)
	if err != nil {
		if isMissingRelation(err) {
			return summary, counts, nil
		}
		return summary, nil, fmt.Errorf("count recent changes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p changePair
		var n int
		if err := rows.Scan(&p.changeType, &p.entityType, &n); err != nil {
			return summary, nil, fmt.Errorf("scan recent change count: %w", err)
		}
		counts[p] += n
		summary.Total += n
		summary.ByChangeType[p.changeType] += n
		summary.ByEntityType[p.entityType] += n
	}
	return summary, counts, rows.Err()
}

// Highlights turns counts into the Overview card's lines, largest first
// (ties: identities before credentials, created before deleted).
func Highlights(counts map[changePair]int) []models.ScanChangeHighlight {
	out := make([]models.ScanChangeHighlight, 0, len(counts))
	for p, n := range counts {
		if n <= 0 {
			continue
		}
		out = append(out, models.ScanChangeHighlight{ChangeType: p.changeType, EntityType: p.entityType, Count: n, Label: highlightLabel(p.changeType, p.entityType, n)})
	}
	rank := func(list []string, v string) int {
		for i, x := range list {
			if x == v {
				return i
			}
		}
		return len(list)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if a, b := rank(models.EntityTypes, out[i].EntityType), rank(models.EntityTypes, out[j].EntityType); a != b {
			return a < b
		}
		return rank(models.ChangeTypes, out[i].ChangeType) < rank(models.ChangeTypes, out[j].ChangeType)
	})
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func highlightLabel(changeType, entityType string, n int) string {
	type words struct{ one, many, created, deleted, modified string }
	w := map[string]words{
		models.EntityIdentity:   {"identity", "identities", "discovered", "removed", "changed"},
		models.EntityCredential: {"credential", "credentials", "discovered", "removed", "changed"},
		models.EntityPermission: {"permission", "permissions", "granted", "revoked", "changed"},
		models.EntityBinding:    {"credential binding", "credential bindings", "added", "removed", "changed"},
		models.EntityTrustEdge:  {"trust relationship", "trust relationships", "added", "removed", "changed"},
	}[entityType]
	if w.one == "" {
		w = words{"change", "changes", "added", "removed", "changed"}
	}
	noun := plural(n, w.one, w.many)
	switch changeType {
	case models.ChangeCreated:
		if entityType == models.EntityIdentity || entityType == models.EntityCredential {
			return fmt.Sprintf("%d new %s %s", n, noun, w.created)
		}
		return fmt.Sprintf("%d %s %s", n, noun, w.created)
	case models.ChangeDeleted:
		return fmt.Sprintf("%d %s %s", n, noun, w.deleted)
	default:
		if entityType == models.EntityPermission {
			return fmt.Sprintf("%d permission %s detected", n, plural(n, "change", "changes"))
		}
		return fmt.Sprintf("%d %s %s", n, noun, w.modified)
	}
}

// DescribeChange is the sentence used when the worker wrote no summary.
func DescribeChange(changeType, entityType, name, arn string) string {
	subject := strings.TrimSpace(name)
	if subject == "" {
		subject = arnTail(arn)
	}
	noun := map[string]string{
		models.EntityIdentity: "Identity", models.EntityCredential: "Credential", models.EntityPermission: "Permission",
		models.EntityBinding: "Credential binding", models.EntityTrustEdge: "Trust relationship",
	}[entityType]
	if noun == "" {
		noun = "Item"
	}
	verb := map[string]string{models.ChangeCreated: "was added", models.ChangeDeleted: "was removed", models.ChangeModified: "was changed"}[changeType]
	if verb == "" {
		verb = "changed"
	}
	if entityType == models.EntityPermission || entityType == models.EntityBinding || entityType == models.EntityTrustEdge {
		return fmt.Sprintf("%s of %s %s", noun, subject, verb)
	}
	return fmt.Sprintf("%s %s %s", noun, subject, verb)
}
