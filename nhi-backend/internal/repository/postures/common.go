// Package postures reads the precomputed ISPM tables (ispm_*) for the posture
// dashboard. The worker writes those tables after every scan; this package
// only reads them (apart from PATCH /findings, which is the one write).
//
// Every query is schema-qualified through the DataScope (s.Table) and filtered
// by tenant_id taken from the JWT. Nothing is hard-coded about the data:
// pillars, rule titles, fixes and categories all come from the catalog tables.
package postures

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	models "nhi-backend-api/internal/models/postures"
	"nhi-backend-api/internal/scope"
)

var (
	// ErrScopeRequired means a repository was called without a tenant scope.
	ErrScopeRequired = errors.New("tenant scope is required")
	// ErrNotFound: the record does not exist for this tenant (also used when
	// it belongs to another tenant, so existence is never revealed).
	ErrNotFound = errors.New("record not found")
	// ErrInvalidCursor: the pagination cursor is malformed or not ours.
	ErrInvalidCursor = errors.New("invalid cursor")
)

// ISPM tables, all in the tenant schema.
const (
	tIdentityPosture = "ispm_identity_posture"
	tIdentityChecks  = "ispm_identity_checks"
	tPostureDaily    = "ispm_identity_posture_daily"
	tPostureHourly   = "ispm_identity_posture_hourly"
	tDashboardCache  = "ispm_nhi_dashboard_cache"
	tSnapshots       = "ispm_posture_snapshots"
	tFindings        = "ispm_nhi_findings"
	tFindingHistory  = "ispm_finding_history"
	tAccessEdges     = "ispm_access_edges"
	tRoleUse         = "ispm_role_use"
	tScanHistory     = "ispm_scan_history"
	tRuleWeights     = "ispm_rule_weights"
	tCategories      = "ispm_categories"
	tPillars         = "ispm_pillars"
)

func need(s *scope.DataScope) error {
	if s == nil || s.DB == nil || strings.TrimSpace(s.TenantID) == "" {
		return ErrScopeRequired
	}
	return nil
}

// isUndefinedTable reports a missing relation (SQLSTATE 42P01). Optional
// sources (role use) answer "no data yet" instead of failing when the table
// has not been created.
func isUndefinedTable(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "42P01"
}

// ─────────────────────────────────────────────────────────────────────────
// Cursor
// ─────────────────────────────────────────────────────────────────────────

func encodeCursor(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(raw string, out any) error {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return ErrInvalidCursor
	}
	if err := json.Unmarshal(b, out); err != nil {
		return ErrInvalidCursor
	}
	return nil
}

// clampLimit applies the documented pagination bounds (default 50, max 100).
func clampLimit(n int) int {
	if n < 1 {
		return 50
	}
	if n > 100 {
		return 100
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────
// Scan metadata (meta.scan_id, ETag)
// ─────────────────────────────────────────────────────────────────────────

const metaCacheTTL = 5 * time.Second

type metaEntry struct {
	meta    models.Meta
	expires time.Time
}

var metaCache = struct {
	sync.Mutex
	m map[string]metaEntry
}{m: map[string]metaEntry{}}

// MetaRepository answers "which scan is the data from". The answer is cached
// for a few seconds per tenant because every request (and every ETag check)
// asks for it, and it only changes once per scan.
type MetaRepository struct{}

func NewMetaRepository() *MetaRepository { return &MetaRepository{} }

// ScanMeta returns the tenant's latest finished scan. DataAvailable is false
// when no scan has been recorded yet.
func (r *MetaRepository) ScanMeta(ctx context.Context, s *scope.DataScope) (models.Meta, error) {
	if err := need(s); err != nil {
		return models.Meta{}, err
	}
	key := s.DBName + "|" + s.TenantID
	now := time.Now()
	metaCache.Lock()
	if e, ok := metaCache.m[key]; ok && now.Before(e.expires) {
		metaCache.Unlock()
		return e.meta, nil
	}
	metaCache.Unlock()

	q := fmt.Sprintf(`SELECT scan_id::text, completed_at, started_at
		FROM %s
		WHERE tenant_id = $1 AND status IN ('COMPLETED', 'PARTIAL')
		ORDER BY started_at DESC NULLS LAST
		LIMIT 1`, s.Table(tScanHistory))
	var id sql.NullString
	var completed, started sql.NullTime
	meta := models.Meta{}
	err := s.DB.QueryRowContext(ctx, q, s.TenantID).Scan(&id, &completed, &started)
	switch {
	case errors.Is(err, sql.ErrNoRows), err != nil && isUndefinedTable(err):
		// no scan yet: empty meta, data_available stays false
	case err != nil:
		return models.Meta{}, fmt.Errorf("load scan meta: %w", err)
	default:
		meta.ScanID = id.String
		meta.DataAvailable = id.Valid && id.String != ""
		if completed.Valid {
			t := completed.Time.UTC()
			meta.ComputedAt = &t
		} else if started.Valid {
			t := started.Time.UTC()
			meta.ComputedAt = &t
		}
	}

	metaCache.Lock()
	if len(metaCache.m) > 10000 {
		metaCache.m = map[string]metaEntry{}
	}
	metaCache.m[key] = metaEntry{meta: meta, expires: now.Add(metaCacheTTL)}
	metaCache.Unlock()
	return meta, nil
}

// ─────────────────────────────────────────────────────────────────────────
// Identity reference (shared by every /identities/{id}/… endpoint)
// ─────────────────────────────────────────────────────────────────────────

// identityRef is the few columns every identity page needs.
type identityRef struct {
	ID    string
	ARN   string
	Name  string
	Type  string
	Class sql.NullString
}

// loadIdentityRef finds the identity in this tenant's posture table, or
// returns ErrNotFound. An identity of another tenant is indistinguishable
// from a missing one.
func loadIdentityRef(ctx context.Context, s *scope.DataScope, id string) (*identityRef, error) {
	if err := need(s); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`SELECT identity_id, COALESCE(identity_arn, ''), COALESCE(identity_name, ''),
			COALESCE(identity_type, ''), functional_class
		FROM %s WHERE tenant_id = $1 AND identity_id = $2`, s.Table(tIdentityPosture))
	var ref identityRef
	err := s.DB.QueryRowContext(ctx, q, s.TenantID, id).Scan(&ref.ID, &ref.ARN, &ref.Name, &ref.Type, &ref.Class)
	if errors.Is(err, sql.ErrNoRows) || isUndefinedTable(err) { // not scored yet
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load identity: %w", err)
	}
	return &ref, nil
}

// ─────────────────────────────────────────────────────────────────────────
// Pillar catalog
// ─────────────────────────────────────────────────────────────────────────

type pillarMeta struct {
	Code  string
	Name  string
	Order int
}

// loadPillarCatalog reads the enabled pillars in display order.
func loadPillarCatalog(ctx context.Context, s *scope.DataScope) ([]pillarMeta, error) {
	q := fmt.Sprintf(`SELECT pillar_code, COALESCE(NULLIF(pillar_name, ''), pillar_code), COALESCE(display_order, 0)
		FROM %s WHERE COALESCE(enabled, true) ORDER BY display_order, pillar_code`, s.Table(tPillars))
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("load pillars: %w", err)
	}
	defer rows.Close()
	out := make([]pillarMeta, 0, 6)
	for rows.Next() {
		var p pillarMeta
		if err := rows.Scan(&p.Code, &p.Name, &p.Order); err != nil {
			return nil, fmt.Errorf("scan pillar: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ─────────────────────────────────────────────────────────────────────────
// Tolerant JSON helpers
//
// The worker stores several dashboard values as JSON. Its exact key spelling
// is read here defensively (a few synonyms per field) so a renamed key does not
// blank the page, and nothing is assumed about which pillars / classes exist.
// ─────────────────────────────────────────────────────────────────────────

func decodeAny(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	case bool:
		return 0, false
	}
	return 0, false
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstOf(m map[string]any, keys ...string) (any, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v, true
		}
	}
	return nil, false
}

func floatOf(m map[string]any, keys ...string) *float64 {
	if v, ok := firstOf(m, keys...); ok {
		if f, ok := asFloat(v); ok {
			return &f
		}
	}
	return nil
}

func intOf(m map[string]any, keys ...string) *int {
	if f := floatOf(m, keys...); f != nil {
		n := int(math.Round(*f))
		return &n
	}
	return nil
}

func stringOf(m map[string]any, keys ...string) string {
	if v, ok := firstOf(m, keys...); ok {
		return asString(v)
	}
	return ""
}

// pickWindow extracts the change for one window from either a plain number or
// a map keyed by window ("30d", "change_30d", "score_change_30d", "1d" …).
func pickWindow(v any, w models.Window) *float64 {
	if v == nil {
		return nil
	}
	if f, ok := asFloat(v); ok {
		return &f
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return floatOf(m, w.Label, w.ChangeSuffix, "change_"+w.Label, "change_"+w.ChangeSuffix,
		"score_change_"+w.Label, "score_change_"+w.ChangeSuffix)
}

// objectList accepts either a JSON array of objects or an object keyed by name
// and returns (name, object) pairs; for arrays the name comes from nameKeys.
type namedObject struct {
	Name string
	Obj  map[string]any
}

func objectList(raw []byte, nameKeys ...string) []namedObject {
	switch t := decodeAny(raw).(type) {
	case []any:
		out := make([]namedObject, 0, len(t))
		for _, it := range t {
			if m, ok := it.(map[string]any); ok {
				out = append(out, namedObject{Name: stringOf(m, nameKeys...), Obj: m})
			}
		}
		return out
	case map[string]any:
		out := make([]namedObject, 0, len(t))
		for k, it := range t {
			switch o := it.(type) {
			case map[string]any:
				out = append(out, namedObject{Name: k, Obj: o})
			default:
				if f, ok := asFloat(o); ok {
					out = append(out, namedObject{Name: k, Obj: map[string]any{"score": f}})
				}
			}
		}
		return out
	}
	return nil
}

var pillarNameKeys = []string{"pillar", "pillar_code", "code", "name"}

// pillarStat is one pillar's numbers as stored by the worker.
type pillarStat struct {
	Score     *float64
	Evaluated *int
	Failing   *int
	Status    string
}

func parsePillarStats(raw []byte) map[string]pillarStat {
	out := map[string]pillarStat{}
	for _, no := range objectList(raw, pillarNameKeys...) {
		if no.Name == "" {
			continue
		}
		out[no.Name] = pillarStat{
			Score:     floatOf(no.Obj, "score", "mean", "mean_score", "pillar_score", "value"),
			Evaluated: intOf(no.Obj, "evaluated", "checks_evaluated"),
			Failing:   intOf(no.Obj, "failing", "failed", "checks_failed"),
			Status:    strings.ToUpper(stringOf(no.Obj, "status", "state")),
		}
	}
	return out
}

// fleetPillarStatus: PASS ≥ 80 · WARN ≥ 50 · FAIL < 50 (mean pillar score).
func fleetPillarStatus(score float64) string {
	switch {
	case score >= 80:
		return "PASS"
	case score >= 50:
		return "WARN"
	}
	return "FAIL"
}

// ─────────────────────────────────────────────────────────────────────────
// Small conversions
// ─────────────────────────────────────────────────────────────────────────

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func nullFloat(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	f := round1(n.Float64)
	return &f
}

func nullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func nullTime(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time.UTC()
	return &t
}

func nullBool(n sql.NullBool) *bool {
	if !n.Valid {
		return nil
	}
	v := n.Bool
	return &v
}

func intVal(n sql.NullInt64) int { return int(n.Int64) }

// daysSince is the whole number of days between t and now (never negative).
func daysSince(t sql.NullTime) *int {
	if !t.Valid {
		return nil
	}
	d := int(time.Since(t.Time).Hours() / 24)
	if d < 0 {
		d = 0
	}
	return &d
}

// humanize turns "least_privilege" into "Least privilege".
func humanize(code string) string {
	code = strings.TrimSpace(strings.ReplaceAll(code, "_", " "))
	if code == "" {
		return ""
	}
	return strings.ToUpper(code[:1]) + code[1:]
}

// titleCase turns "ACTIVE" into "Active".
func titleCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	return strings.ToUpper(s[:1]) + s[1:]
}

// tagList reads a compliance-tag value stored as a JSON array of strings (or
// of objects with a tag / name / id field).
func tagList(raw []byte) []string {
	arr, ok := decodeAny(raw).([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	seen := map[string]struct{}{}
	for _, it := range arr {
		tag := ""
		switch t := it.(type) {
		case string:
			tag = strings.TrimSpace(t)
		case map[string]any:
			tag = stringOf(t, "tag", "name", "id", "control")
		}
		if tag == "" {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

// escapeLike escapes %, _ and \ so user text is matched literally by LIKE.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// nullableFloat scans a numeric column that may be NULL and exposes it as a
// rounded *float64.
type nullableFloat struct{ sql.NullFloat64 }

func (n nullableFloat) ptr() *float64 { return nullFloat(n.NullFloat64) }

// accessKeyMarkers are the ways an access key's synthetic ARN names the key:
// IAM access keys have no AWS ARN, so scanners write their own -
// "…:user/name/access-key/AKIA…" (this API's documentation) or
// "…/accesskey/AKIA…" (the discovery inventory).
var accessKeyMarkers = []string{"access-key/", "accesskey/", "access_key/"}

// accessKeyID returns the key id inside an access key's ARN.
func accessKeyID(arn string) (string, bool) {
	lower := strings.ToLower(arn)
	for _, m := range accessKeyMarkers {
		if i := strings.LastIndex(lower, m); i >= 0 {
			if id := arn[i+len(m):]; id != "" && !strings.Contains(id, "/") {
				return id, true
			}
		}
	}
	return "", false
}
