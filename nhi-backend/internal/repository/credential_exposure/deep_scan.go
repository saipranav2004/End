package credential_exposure

import (
	"context"
	"database/sql"
	"fmt"

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
