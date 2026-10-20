package credential_exposure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	models "nhi-backend-api/internal/models/credential_exposure"
	"nhi-backend-api/internal/scope"
)

type DeepScanRepository struct{}

func NewDeepScanRepository() *DeepScanRepository { return &DeepScanRepository{} }

func (r *DeepScanRepository) Status(ctx context.Context, resolved *scope.DataScope, clientID, repoKey, platform string) ([]models.DeepScanResult, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, errScopeRequired
	}
	schema := resolved.QSchema()
	query := `SELECT client_id,repo_key,platform,requested_at,completed_at,commits_scanned,findings,failed_at,error,last_commit_id,updated_at,pending_commit_id,pending_since
FROM (
 SELECT d.client_id,d.repo_key,d.platform,d.requested_at,d.completed_at,d.commits_scanned,d.findings,d.failed_at,d.error,l.last_commit_id,l.updated_at,l.pending_commit_id,l.pending_since
 FROM ` + schema + `.secscan_deepscan_codecommit d LEFT JOIN ` + schema + `.secscan_ledger_codecommit l USING (client_id,repo_key,platform)
 UNION ALL
 SELECT l.client_id,l.repo_key,l.platform,NULL::varchar,NULL::varchar,NULL::integer,NULL::integer,NULL::varchar,NULL::varchar,l.last_commit_id,l.updated_at,l.pending_commit_id,l.pending_since
 FROM ` + schema + `.secscan_ledger_codecommit l LEFT JOIN ` + schema + `.secscan_deepscan_codecommit d USING (client_id,repo_key,platform) WHERE d.client_id IS NULL
 UNION ALL
 SELECT d.client_id,d.repo_key,d.platform,d.requested_at,d.completed_at,d.commits_scanned,d.findings,d.failed_at,d.error,l.last_commit_id,l.updated_at,l.pending_commit_id,l.pending_since
 FROM ` + schema + `.secscan_deepscan_github d LEFT JOIN ` + schema + `.secscan_ledger_github l USING (client_id,repo_key,platform)
 UNION ALL
 SELECT l.client_id,l.repo_key,l.platform,NULL::varchar,NULL::varchar,NULL::integer,NULL::integer,NULL::varchar,NULL::varchar,l.last_commit_id,l.updated_at,l.pending_commit_id,l.pending_since
 FROM ` + schema + `.secscan_ledger_github l LEFT JOIN ` + schema + `.secscan_deepscan_github d USING (client_id,repo_key,platform) WHERE d.client_id IS NULL
) scan_state
WHERE client_id=$1 AND repo_key=$2 AND ($3='' OR LOWER(platform)=LOWER($3))
ORDER BY platform`
	rows, err := resolved.DB.QueryContext(ctx, query, clientID, repoKey, platform)
	if err != nil {
		return nil, fmt.Errorf("query scanner repository status: %w", err)
	}
	defer rows.Close()
	out := make([]models.DeepScanResult, 0)
	for rows.Next() {
		var item models.DeepScanResult
		var requested, completed, failed, errText, lastCommit, updated, pendingCommit, pendingSince sql.NullString
		var commits, findings sql.NullInt64
		if err := rows.Scan(&item.ClientID, &item.RepoKey, &item.Platform, &requested, &completed, &commits, &findings, &failed, &errText, &lastCommit, &updated, &pendingCommit, &pendingSince); err != nil {
			return nil, fmt.Errorf("scan repository status: %w", err)
		}
		item.RequestedAt, item.CompletedAt, item.FailedAt, item.Error = strPtr(requested), strPtr(completed), strPtr(failed), strPtr(errText)
		item.CommitsScanned, item.Findings = intPtr(commits), intPtr(findings)
		item.LastCommitID, item.UpdatedAt, item.PendingCommitID, item.PendingSince = strPtr(lastCommit), strPtr(updated), strPtr(pendingCommit), strPtr(pendingSince)
		item.Status = "not_started"
		if item.RequestedAt != nil {
			item.Status = "running"
		}
		if item.FailedAt != nil {
			item.Status = "failed"
		} else if item.CompletedAt != nil {
			item.Status = "complete"
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository status: %w", err)
	}
	return out, nil
}

// ErrDeepScanClientNotFound: this tenant's scanner tables have never seen the
// client, so there is no credential the worker could scan it with.
var ErrDeepScanClientNotFound = errors.New("deep scan: unknown client_id")

// deepScanPlatforms are the platforms whose secscan_* tables exist per tenant.
var deepScanPlatforms = []string{"codecommit", "github"}

// ClientPlatforms returns the platforms on which this tenant's scanner has a
// ledger row or a finding for clientID. A schema without a platform's tables
// simply does not list it.
func (r *DeepScanRepository) ClientPlatforms(ctx context.Context, resolved *scope.DataScope, clientID string) ([]string, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, errScopeRequired
	}
	schema := resolved.QSchema()
	var out []string
	for _, p := range deepScanPlatforms {
		var known bool
		q := `SELECT EXISTS (SELECT 1 FROM ` + schema + `.secscan_ledger_` + p + ` WHERE client_id = $1)
		          OR EXISTS (SELECT 1 FROM ` + schema + `.secscan_findings_` + p + ` WHERE client_id = $1)`
		if err := resolved.DB.QueryRowContext(ctx, q, clientID).Scan(&known); err != nil {
			if isMissingRelation(err) {
				continue
			}
			return nil, fmt.Errorf("look up scanner client: %w", err)
		}
		if known {
			out = append(out, p)
		}
	}
	return out, nil
}

// Request asks the scanner worker for a full-history walk of one repository
// by writing its secscan_deepscan_<platform> row: requested_at set, the last
// run's result cleared. The worker picks up rows with requested_at set and
// neither completed_at nor failed_at (README, "Deep-scan contract"). A walk
// already waiting or running is left alone and reported as such - one
// statement, so two clicks never reset each other. requestedAt is the
// scanner's own string format.
func (r *DeepScanRepository) Request(ctx context.Context, resolved *scope.DataScope, clientID, repoKey, platform string, now time.Time) (requestedAt string, alreadyRunning bool, err error) {
	if resolved == nil || resolved.DB == nil {
		return "", false, errScopeRequired
	}
	if platform != "codecommit" && platform != "github" {
		return "", false, fmt.Errorf("deep scan: unsupported platform %q", platform)
	}
	table := resolved.QSchema() + `.secscan_deepscan_` + platform
	q := `INSERT INTO ` + table + ` AS d (client_id, repo_key, platform, requested_at)
	      VALUES ($1, $2, $3, $4)
	      ON CONFLICT (client_id, repo_key, platform) DO UPDATE
	         SET requested_at = EXCLUDED.requested_at, completed_at = NULL, commits_scanned = NULL,
	             findings = NULL, failed_at = NULL, error = NULL
	       WHERE d.completed_at IS NOT NULL OR d.failed_at IS NOT NULL
	      RETURNING d.requested_at`
	err = resolved.DB.QueryRowContext(ctx, q, clientID, repoKey, platform, dashboardTimestamp(now)).Scan(&requestedAt)
	if err == nil {
		return requestedAt, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("request deep scan: %w", err)
	}
	// The conflict row is still waiting or running: report its request time.
	if err := resolved.DB.QueryRowContext(ctx, `SELECT requested_at FROM `+table+`
	      WHERE client_id = $1 AND repo_key = $2 AND platform = $3`, clientID, repoKey, platform).Scan(&requestedAt); err != nil {
		return "", false, fmt.Errorf("read running deep scan: %w", err)
	}
	return requestedAt, true, nil
}

// isMissingRelation reports undefined_table (42P01).
func isMissingRelation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}
