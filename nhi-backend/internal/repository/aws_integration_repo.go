package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"

	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/database"
	modelsv1 "nhi-backend-api/internal/models/v1"
)

// The AWS integration lives in one table per tenant schema: accounts. It is
// both the scope table every screen filters by (one row per account, regions
// text[], is_default) and the connection record (client_role_arn,
// external_id, is_connected, account_status, ...). The connection columns
// beyond the scanner's are added by migrations/tenant/003_accounts_connection.sql.
// globalRegion is the scanner's synthetic region for global resources (IAM).
const globalRegion = "global"

const accountsTable = "accounts"

// connectionRow limits reads to accounts someone has connected through the
// Integrations page; scanner-only scope rows carry no role.
const connectionRow = `client_role_arn IS NOT NULL AND client_role_arn <> ''`

var (
	// ErrAWSAccountClaimed: the AWS account is connected to another tenant.
	ErrAWSAccountClaimed = errors.New("aws account is connected to another tenant")
	// ErrAWSAccountNotFound: the tenant has no such account.
	ErrAWSAccountNotFound = errors.New("aws account not found")
	// ErrAWSAccountNotConnected: the account has not passed all checks.
	ErrAWSAccountNotConnected = errors.New("aws account is not connected")
	// ErrAWSTablesMissing: a migration has not been applied.
	ErrAWSTablesMissing = errors.New("aws integration schema is missing: apply migrations/global/004_tenant_collector.sql and, per tenant schema, migrations/tenant/003_accounts_connection.sql")
)

// missingSchema maps undefined_table (42P01) and undefined_column (42703) to
// ErrAWSTablesMissing, so a deployment that skipped a migration says so.
func missingSchema(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "42P01" || pgErr.Code == "42703") {
		return fmt.Errorf("%w (%s)", ErrAWSTablesMissing, pgErr.Message)
	}
	return err
}

// AWSIntegrationRepository reads the tenant's collector values from
// nhi_global.tenants and keeps the connection columns of <tenant
// schema>.accounts. Every tenant query is schema-qualified
// with the schema from the caller's token.
type AWSIntegrationRepository struct {
	db           *PostgresDB
	globalSchema string
}

func NewAWSIntegrationRepository(db *PostgresDB) *AWSIntegrationRepository {
	schema := config.DefaultGlobalSchema
	if db != nil && strings.TrimSpace(db.GlobalSchema()) != "" {
		schema = db.GlobalSchema()
	}
	return &AWSIntegrationRepository{db: db, globalSchema: schema}
}

func (r *AWSIntegrationRepository) pool() (*sql.DB, error) {
	if r == nil || r.db == nil || r.db.DB == nil {
		return nil, errors.New("aws integration repository is not initialized")
	}
	return r.db.DB, nil
}

// tenantPool validates the tenant schema name and returns the shared pool.
func (r *AWSIntegrationRepository) tenantPool(schema string) (*sql.DB, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("aws integration repository is not initialized")
	}
	return r.db.GetTenantDB(schema)
}

// TenantSettings returns the tenant's collector values, or nil when the
// tenant does not exist. Unset columns come back empty. collector_regions is
// read through to_jsonb so a text[] or jsonb column both work.
func (r *AWSIntegrationRepository) TenantSettings(ctx context.Context, tenantID string) (*modelsv1.AWSTenantSettings, error) {
	db, err := r.pool()
	if err != nil {
		return nil, err
	}
	q := `SELECT COALESCE(domain_name, ''), COALESCE(collector_aws_account_id, ''), COALESCE(collector_external_id, ''),
			COALESCE(collector_event_bridge_arn, ''), COALESCE(to_jsonb(collector_regions), '[]'::jsonb)::text
		FROM ` + database.Table(r.globalSchema, "tenants") + ` WHERE tenant_id = $1::uuid`
	var s modelsv1.AWSTenantSettings
	var regions string
	err = db.QueryRowContext(ctx, q, tenantID).Scan(&s.Domain, &s.AWSAccountID, &s.ExternalID, &s.EventBusARN, &regions)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load tenant collector settings: %w", missingSchema(err))
	}
	s.Domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s.Domain)), "@")
	s.AWSAccountID = strings.TrimSpace(s.AWSAccountID)
	s.ExternalID = strings.TrimSpace(s.ExternalID)
	s.EventBusARN = strings.TrimSpace(s.EventBusARN)
	s.Regions = parseRegionList(regions)
	return &s, nil
}

// parseRegionList accepts a JSON array, or a JSON string holding a
// comma-separated list, and returns trimmed lower-case codes.
func parseRegionList(raw string) []string {
	out := []string{}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		var single string
		if json.Unmarshal([]byte(raw), &single) != nil {
			return out
		}
		list = strings.Split(single, ",")
	}
	for _, v := range list {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// accountColumns reads one accounts row as an AWSAccountRecord. The scanner's
// names are github_enabled / codecommit_enabled; regions carry 'global',
// which is stripped in scanAccount (customers pick real regions only).
const accountColumns = `account_id, COALESCE(client_role_arn, ''), COALESCE(regions, '{}'::text[]), COALESCE(external_id, ''),
	account_status, is_connected, checks,
	COALESCE(last_verified_at, updated_at), last_verified_by, connected_at,
	github_enabled, COALESCE(github_secret_arn, ''), github_checks, github_verified_at,
	codecommit_enabled, codecommit_checks, codecommit_verified_at, exposure_updated_by,
	sync_status, sync_requested_at, sync_requested_by, sync_started_at, last_synced_at, last_sync_error`

type rowScanner interface{ Scan(dest ...any) error }

func decodeChecks(raw []byte) ([]modelsv1.AWSCheck, error) {
	out := make([]modelsv1.AWSCheck, 0)
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode checks: %w", err)
	}
	return out, nil
}

func scanAccount(row rowScanner) (*modelsv1.AWSAccountRecord, error) {
	var rec modelsv1.AWSAccountRecord
	var regions pq.StringArray
	var checks, ghChecks, ccChecks []byte
	var connectedAt, ghVerified, ccVerified, requestedAt, startedAt, syncedAt sql.NullTime
	if err := row.Scan(&rec.AccountID, &rec.RoleARN, &regions, &rec.ExternalID, &rec.Status, &rec.IsConnected, &checks,
		&rec.LastVerifiedAt, &rec.LastVerifiedBy, &connectedAt,
		&rec.GitHubConnected, &rec.GitHubSecretARN, &ghChecks, &ghVerified,
		&rec.CodeCommitConnected, &ccChecks, &ccVerified, &rec.ExposureUpdatedBy,
		&rec.SyncStatus, &requestedAt, &rec.SyncRequestedBy, &startedAt, &syncedAt, &rec.LastSyncError); err != nil {
		return nil, err
	}
	rec.Regions = withoutGlobal(regions)
	var err error
	if rec.Checks, err = decodeChecks(checks); err != nil {
		return nil, err
	}
	if rec.GitHubChecks, err = decodeChecks(ghChecks); err != nil {
		return nil, err
	}
	if rec.CodeCommitChecks, err = decodeChecks(ccChecks); err != nil {
		return nil, err
	}
	rec.LastVerifiedAt = rec.LastVerifiedAt.UTC()
	rec.ConnectedAt = utcPtr(connectedAt)
	rec.GitHubVerified = utcPtr(ghVerified)
	rec.CodeCommitVerified = utcPtr(ccVerified)
	rec.SyncRequestedAt = utcPtr(requestedAt)
	rec.SyncStartedAt = utcPtr(startedAt)
	rec.LastSyncedAt = utcPtr(syncedAt)
	return &rec, nil
}

func utcPtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

// ListAccounts returns the tenant's AWS accounts, connected first.
func (r *AWSIntegrationRepository) ListAccounts(ctx context.Context, schema string) ([]modelsv1.AWSAccountRecord, error) {
	db, err := r.tenantPool(schema)
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + accountColumns + ` FROM ` + database.Table(schema, accountsTable) + `
		WHERE ` + connectionRow + `
		ORDER BY is_connected DESC, created_at, account_id`
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list aws accounts: %w", missingSchema(err))
	}
	defer rows.Close()
	out := make([]modelsv1.AWSAccountRecord, 0)
	for rows.Next() {
		rec, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("scan aws account: %w", err)
		}
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aws accounts: %w", err)
	}
	applyLastScans(ctx, db, schema, out)
	return out, nil
}

// applyLastScans makes the scanner's own completed runs count as syncs: the
// scanner records them in nhi_scans_aws and never touches last_synced_at, so
// without this an account it scanned yesterday reads "not synced yet". Best
// effort - a schema without the scanner's table keeps last_synced_at as is.
func applyLastScans(ctx context.Context, db *sql.DB, schema string, accounts []modelsv1.AWSAccountRecord) {
	rows, err := db.QueryContext(ctx, `SELECT account_id::text, max(scan_end) FROM `+database.Table(schema, "nhi_scans_aws")+`
		WHERE upper(status) = 'COMPLETED' AND scan_end IS NOT NULL GROUP BY 1`)
	if err != nil {
		return
	}
	defer rows.Close()
	last := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if rows.Scan(&id, &at) == nil {
			last[id] = at.UTC()
		}
	}
	for i := range accounts {
		at, ok := last[accounts[i].AccountID]
		if ok && (accounts[i].LastSyncedAt == nil || at.After(*accounts[i].LastSyncedAt)) {
			accounts[i].LastSyncedAt = &at
			if accounts[i].SyncStatus == "idle" {
				accounts[i].SyncStatus = "succeeded"
			}
		}
	}
}

// GetAccount returns one of the tenant's accounts, or nil.
func (r *AWSIntegrationRepository) GetAccount(ctx context.Context, schema, accountID string) (*modelsv1.AWSAccountRecord, error) {
	db, err := r.tenantPool(schema)
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + accountColumns + ` FROM ` + database.Table(schema, accountsTable) + `
		WHERE account_id = $1 AND ` + connectionRow
	rec, err := scanAccount(db.QueryRowContext(ctx, q, accountID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load aws account: %w", missingSchema(err))
	}
	return rec, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// AccountClaimedByOther reports whether the AWS account is saved by any
// tenant other than tenantID. One AWS account belongs to one tenant: the
// event pipeline maps an event's account to a tenant, so a shared account
// would leak one tenant's activity into another.
func (r *AWSIntegrationRepository) AccountClaimedByOther(ctx context.Context, tenantID, accountID string) (bool, error) {
	db, err := r.pool()
	if err != nil {
		return false, err
	}
	return r.claimedByOther(ctx, db, tenantID, accountID)
}

func (r *AWSIntegrationRepository) claimedByOther(ctx context.Context, q queryer, tenantID, accountID string) (bool, error) {
	// The other tenants' schemas whose accounts table has the connection
	// columns (a schema not yet migrated cannot hold a connection).
	rows, err := q.QueryContext(ctx, `SELECT t.db_name
		FROM `+database.Table(r.globalSchema, "tenants")+` t
		JOIN pg_namespace n ON n.nspname = t.db_name
		JOIN pg_class c ON c.relnamespace = n.oid AND c.relname = $2 AND c.relkind IN ('r', 'p')
		WHERE t.tenant_id <> $1::uuid
		  AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'client_role_arn' AND NOT a.attisdropped)`,
		tenantID, accountsTable)
	if err != nil {
		return false, fmt.Errorf("list tenant schemas: %w", missingSchema(err))
	}
	schemas := make([]string, 0)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return false, fmt.Errorf("scan tenant schema: %w", err)
		}
		if config.ValidateSchemaName(s) == nil {
			schemas = append(schemas, s)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate tenant schemas: %w", err)
	}
	if len(schemas) == 0 {
		return false, nil
	}
	parts := make([]string, len(schemas))
	for i, s := range schemas {
		parts[i] = `SELECT 1 FROM ` + database.Table(s, accountsTable) + ` WHERE account_id = $1 AND ` + connectionRow
	}
	var one int
	err = q.QueryRowContext(ctx, `SELECT 1 FROM (`+strings.Join(parts, " UNION ALL ")+`) owners LIMIT 1`, accountID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check aws account owner: %w", missingSchema(err))
	}
	return true, nil
}

// lockAccount serializes every write about one AWS account across tenants,
// so two tenants cannot both claim it and registrations cannot interleave.
func lockAccount(ctx context.Context, tx *sql.Tx, accountID string) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('nhi_aws_account:' || $1))`, accountID); err != nil {
		return fmt.Errorf("lock aws account: %w", err)
	}
	return nil
}

// SaveVerification records the outcome of a verification on the account's
// accounts row (added when missing). connected_at is set the first time the
// account passes and kept afterwards; that first time a sync is queued. The
// row's regions always include 'global' so IAM identities stay in scope.
func (r *AWSIntegrationRepository) SaveVerification(ctx context.Context, tenantID, schema string, rec modelsv1.AWSAccountRecord) (saved *modelsv1.AWSAccountRecord, syncQueued bool, err error) {
	db, err := r.tenantPool(schema)
	if err != nil {
		return nil, false, err
	}
	checks, err := json.Marshal(nonNil(rec.Checks))
	if err != nil {
		return nil, false, fmt.Errorf("encode checks: %w", err)
	}
	table := database.Table(schema, accountsTable)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockAccount(ctx, tx, rec.AccountID); err != nil {
		return nil, false, err
	}
	claimed, err := r.claimedByOther(ctx, tx, tenantID, rec.AccountID)
	if err != nil {
		return nil, false, err
	}
	if claimed {
		return nil, false, ErrAWSAccountClaimed
	}

	var prevConnected sql.NullTime
	var prevSync string
	err = tx.QueryRowContext(ctx, `SELECT connected_at, sync_status FROM `+table+`
		WHERE account_id = $1 FOR UPDATE`, rec.AccountID).Scan(&prevConnected, &prevSync)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("lock aws account row: %w", missingSchema(err))
	}
	passed := rec.Status == modelsv1.AWSStatusConnected
	queue := passed && !prevConnected.Valid &&
		prevSync != modelsv1.AWSSyncQueued && prevSync != modelsv1.AWSSyncRunning

	// $5 is the outcome of this check (connected / not). account_status:
	// connected when it passed; action_needed when it failed after having
	// connected before; pending otherwise.
	q := `INSERT INTO ` + table + ` AS a
			(tenant_id, account_id, is_default, client_aws_account_id, client_role_arn, regions, external_id,
			 is_connected, account_status, checks,
			 last_verified_at, last_verified_by, connected_at,
			 sync_status, sync_requested_at, sync_requested_by, updated_at)
		VALUES ($10::uuid, $1::text, true, $1::text, $2::text, $3::text[], $4::text, $5, CASE WHEN $5 THEN 'connected' ELSE 'pending' END, $6::jsonb, $7, $8,
			CASE WHEN $5 THEN $7::timestamptz END,
			CASE WHEN $9 THEN 'queued' ELSE 'idle' END,
			CASE WHEN $9 THEN $7::timestamptz END,
			CASE WHEN $9 THEN $8 ELSE '' END,
			now())
		ON CONFLICT (account_id) DO UPDATE SET
			client_aws_account_id = EXCLUDED.client_aws_account_id,
			client_role_arn = EXCLUDED.client_role_arn,
			regions = EXCLUDED.regions,
			external_id = EXCLUDED.external_id,
			is_connected = EXCLUDED.is_connected,
			account_status = CASE WHEN EXCLUDED.is_connected THEN 'connected'
			                      WHEN a.connected_at IS NOT NULL THEN 'action_needed'
			                      ELSE 'pending' END,
			checks = EXCLUDED.checks,
			last_verified_at = EXCLUDED.last_verified_at,
			last_verified_by = EXCLUDED.last_verified_by,
			connected_at = COALESCE(a.connected_at, EXCLUDED.connected_at),
			sync_status = CASE WHEN $9 THEN 'queued' ELSE a.sync_status END,
			sync_requested_at = CASE WHEN $9 THEN EXCLUDED.sync_requested_at ELSE a.sync_requested_at END,
			sync_requested_by = CASE WHEN $9 THEN EXCLUDED.sync_requested_by ELSE a.sync_requested_by END,
			last_sync_error = CASE WHEN $9 THEN '' ELSE a.last_sync_error END,
			updated_at = now()
		RETURNING ` + accountColumns
	saved, err = scanAccount(tx.QueryRowContext(ctx, q, rec.AccountID, rec.RoleARN, pq.Array(scopeRegions(rec.Regions)),
		rec.ExternalID, passed, checks, rec.LastVerifiedAt, rec.LastVerifiedBy, queue, tenantID))
	if err != nil {
		return nil, false, fmt.Errorf("save aws account: %w", missingSchema(err))
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit aws account: %w", err)
	}
	return saved, queue, nil
}

// UpdateRegions replaces the regions an account is read in ('global' kept).
func (r *AWSIntegrationRepository) UpdateRegions(ctx context.Context, tenantID, schema, accountID string, regions []string) (*modelsv1.AWSAccountRecord, error) {
	db, err := r.tenantPool(schema)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockAccount(ctx, tx, accountID); err != nil {
		return nil, err
	}
	q := `UPDATE ` + database.Table(schema, accountsTable) + `
		SET regions = $2::text[], updated_at = now()
		WHERE account_id = $1 AND tenant_id = $3::uuid AND ` + connectionRow + `
		RETURNING ` + accountColumns
	rec, err := scanAccount(tx.QueryRowContext(ctx, q, accountID, pq.Array(scopeRegions(regions)), tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAWSAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update aws account regions: %w", missingSchema(err))
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return rec, nil
}

// ExposureUpdate is the outcome of an exposed-credential change for one
// account. A nil platform pointer leaves that platform as it is.
type ExposureUpdate struct {
	GitHub     *ExposurePlatformUpdate
	CodeCommit *ExposurePlatformUpdate
	Actor      string
	At         time.Time
}

type ExposurePlatformUpdate struct {
	Connected bool
	Checks    []modelsv1.AWSCheck
	// SecretARN is stored for GitHub; empty clears it.
	SecretARN string
}

// SaveExposure stores the exposed-credential state of one account.
func (r *AWSIntegrationRepository) SaveExposure(ctx context.Context, schema, accountID string, u ExposureUpdate) (*modelsv1.AWSAccountRecord, error) {
	db, err := r.tenantPool(schema)
	if err != nil {
		return nil, err
	}
	sets := []string{"exposure_updated_by = $2", "updated_at = now()"}
	args := []any{accountID, u.Actor}
	add := func(expr string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf(expr, len(args)))
	}
	if p := u.GitHub; p != nil {
		checks, err := json.Marshal(nonNil(p.Checks))
		if err != nil {
			return nil, fmt.Errorf("encode github checks: %w", err)
		}
		add("github_enabled = $%d", p.Connected)
		add("github_checks = $%d::jsonb", checks)
		add("github_secret_arn = NULLIF($%d, '')", p.SecretARN)
		if len(p.Checks) > 0 {
			add("github_verified_at = $%d", u.At)
		} else {
			sets = append(sets, "github_verified_at = NULL")
		}
	}
	if p := u.CodeCommit; p != nil {
		checks, err := json.Marshal(nonNil(p.Checks))
		if err != nil {
			return nil, fmt.Errorf("encode codecommit checks: %w", err)
		}
		add("codecommit_enabled = $%d", p.Connected)
		add("codecommit_checks = $%d::jsonb", checks)
		if len(p.Checks) > 0 {
			add("codecommit_verified_at = $%d", u.At)
		} else {
			sets = append(sets, "codecommit_verified_at = NULL")
		}
	}
	q := `UPDATE ` + database.Table(schema, accountsTable) + ` SET ` + strings.Join(sets, ", ") + `
		WHERE account_id = $1 AND ` + connectionRow + ` RETURNING ` + accountColumns
	rec, err := scanAccount(db.QueryRowContext(ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAWSAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("save exposure: %w", missingSchema(err))
	}
	return rec, nil
}

// RequestSync queues a sync of a connected account. When one is already
// queued or running it changes nothing and reports alreadyQueued.
func (r *AWSIntegrationRepository) RequestSync(ctx context.Context, schema, accountID, actor string, at time.Time) (rec *modelsv1.AWSAccountRecord, alreadyQueued bool, err error) {
	db, err := r.tenantPool(schema)
	if err != nil {
		return nil, false, err
	}
	q := `UPDATE ` + database.Table(schema, accountsTable) + `
		SET sync_status = 'queued', sync_requested_at = $2, sync_requested_by = $3,
			last_sync_error = '', updated_at = now()
		WHERE account_id = $1 AND ` + connectionRow + `
		  AND is_connected AND sync_status NOT IN ('queued', 'running')
		RETURNING ` + accountColumns
	rec, err = scanAccount(db.QueryRowContext(ctx, q, accountID, at, actor))
	if err == nil {
		return rec, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("queue aws sync: %w", missingSchema(err))
	}
	// Nothing updated: say why.
	current, err := r.GetAccount(ctx, schema, accountID)
	if err != nil {
		return nil, false, err
	}
	switch {
	case current == nil:
		return nil, false, ErrAWSAccountNotFound
	case !current.IsConnected:
		return current, false, ErrAWSAccountNotConnected
	default:
		return current, true, nil
	}
}

// DeleteAccount removes the account's accounts row, which also takes it out
// of every scope. Records already scanned stay in the tenant schema.
func (r *AWSIntegrationRepository) DeleteAccount(ctx context.Context, tenantID, schema, accountID string) error {
	db, err := r.tenantPool(schema)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockAccount(ctx, tx, accountID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM `+database.Table(schema, accountsTable)+`
		WHERE tenant_id = $1::uuid AND account_id = $2 AND `+connectionRow, tenantID, accountID)
	if err != nil {
		return fmt.Errorf("delete aws account: %w", missingSchema(err))
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted aws accounts: %w", err)
	}
	if n == 0 {
		return ErrAWSAccountNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// scopeRegions is what accounts.regions stores: the chosen regions plus the
// synthetic "global" region (IAM roles, users and other global resources are
// recorded under it; without it a connected account would show none of its
// IAM identities), de-duplicated and sorted.
func scopeRegions(regions []string) []string {
	seen := map[string]bool{globalRegion: true}
	out := []string{globalRegion}
	for _, r := range regions {
		if r = strings.TrimSpace(r); r != "" && !seen[strings.ToLower(r)] {
			seen[strings.ToLower(r)] = true
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// withoutGlobal is the inverse for the API: the regions a customer chose.
func withoutGlobal(regions []string) []string {
	out := make([]string, 0, len(regions))
	for _, r := range regions {
		if !strings.EqualFold(strings.TrimSpace(r), globalRegion) && strings.TrimSpace(r) != "" {
			out = append(out, r)
		}
	}
	return out
}

func nonNil(c []modelsv1.AWSCheck) []modelsv1.AWSCheck {
	if c == nil {
		return []modelsv1.AWSCheck{}
	}
	return c
}
