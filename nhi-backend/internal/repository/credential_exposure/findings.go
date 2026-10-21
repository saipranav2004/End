package credential_exposure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	models "nhi-backend-api/internal/models/credential_exposure"
	"nhi-backend-api/internal/scope"
)

var (
	ErrFindingNotFound  = errors.New("finding not found")
	ErrFindingAmbiguous = errors.New("finding id exists in both platform tables")
	errScopeRequired    = errors.New("tenant scope is required")
)

type FindingsRepository struct{}

func NewFindingsRepository() *FindingsRepository { return &FindingsRepository{} }

const findingColumns = `finding_id, client_id, repository, branch, commit_id, author, created_at,
 detector, severity, file_path, line_number, redacted, verification_status, risk_tier,
 recommended_action, codecommit_uri, platform, github_uri, line_preview, confidence,
 category, committer, commit_message, commit_authored_at, files_changed, additions,
 deletions, repo_visibility, repo_stars, repo_forks, repo_pushed_at, ml_score,
 ml_decision, ml_model_version, analyst_decision, analyst_decided_at, analyst_decided_by`

func findingsUnion(schema string) string {
	// secscan tables live in the tenant schema (passed in already quoted),
	// same as other scanner tables. They are tenant-isolated by schema, not by
	// account_id. Client_id filtering is still supported for multi-client tenants.
	return `SELECT ` + findingColumns + ` FROM (
 SELECT finding_id,client_id,repository,branch,commit_id,author,created_at,detector,severity,file_path,line_number,redacted,verification_status,risk_tier,recommended_action,codecommit_uri,COALESCE(NULLIF(platform,''),'codecommit') AS platform,github_uri,line_preview,confidence,category,committer,commit_message,commit_authored_at,files_changed,additions,deletions,repo_visibility,repo_stars,repo_forks,repo_pushed_at,ml_score,ml_decision,ml_model_version,analyst_decision,analyst_decided_at,analyst_decided_by
 FROM ` + schema + `.secscan_findings_codecommit f
 WHERE NOT EXISTS (SELECT 1 FROM ` + schema + `.secscan_allowlist_dashboard a WHERE a.client_id=f.client_id AND a.file_path=f.file_path AND a.detector=f.detector AND a.redacted=f.redacted)
 UNION ALL
 SELECT finding_id,client_id,repository,branch,commit_id,author,created_at,detector,severity,file_path,line_number,redacted,verification_status,risk_tier,recommended_action,codecommit_uri,COALESCE(NULLIF(platform,''),'github') AS platform,github_uri,line_preview,confidence,category,committer,commit_message,commit_authored_at,files_changed,additions,deletions,repo_visibility,repo_stars,repo_forks,repo_pushed_at,ml_score,ml_decision,ml_model_version,analyst_decision,analyst_decided_at,analyst_decided_by
 FROM ` + schema + `.secscan_findings_github f
 WHERE NOT EXISTS (SELECT 1 FROM ` + schema + `.secscan_allowlist_dashboard a WHERE a.client_id=f.client_id AND a.file_path=f.file_path AND a.detector=f.detector AND a.redacted=f.redacted)
) AS findings`
}

func (r *FindingsRepository) List(ctx context.Context, resolved *scope.DataScope, filter models.FindingsFilter) ([]models.Finding, int, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, 0, errScopeRequired
	}
	clause, args := findingsWhere(filter.ClientID, filter.Platform, filter.MLFlagged)
	base := findingsUnion(resolved.QSchema())
	var total int
	if err := resolved.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+base+") f"+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count credential exposure findings: %w", err)
	}
	listArgs := append([]any(nil), args...)
	listArgs = append(listArgs, filter.Limit, filter.Offset)
	query := "SELECT " + findingColumns + " FROM (" + base + ") f" + clause +
		" ORDER BY " + scannerTimeOrder("created_at") + fmt.Sprintf(", finding_id ASC LIMIT $%d OFFSET $%d", len(listArgs)-1, len(listArgs))
	rows, err := resolved.DB.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query credential exposure findings: %w", err)
	}
	defer rows.Close()
	items := make([]models.Finding, 0)
	for rows.Next() {
		item, err := scanFinding(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan credential exposure finding: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate credential exposure findings: %w", err)
	}
	return items, total, nil
}

func (r *FindingsRepository) GetByID(ctx context.Context, resolved *scope.DataScope, id string) (*models.Finding, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, errScopeRequired
	}
	rows, err := resolved.DB.QueryContext(ctx, "SELECT "+findingColumns+" FROM ("+findingsUnion(resolved.QSchema())+") f WHERE finding_id=$1 LIMIT 2", id)
	if err != nil {
		return nil, fmt.Errorf("query finding: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate finding: %w", err)
		}
		return nil, ErrFindingNotFound
	}
	item, err := scanFinding(rows)
	if err != nil {
		return nil, fmt.Errorf("scan finding: %w", err)
	}
	if rows.Next() {
		return nil, ErrFindingAmbiguous
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finding: %w", err)
	}
	return &item, nil
}

func (r *FindingsRepository) Summary(ctx context.Context, resolved *scope.DataScope, filter models.FindingsSummaryFilter) (models.FindingsSummary, error) {
	if resolved == nil || resolved.DB == nil {
		return models.FindingsSummary{}, errScopeRequired
	}
	clause, args := findingsWhere(filter.ClientID, filter.Platform, filter.MLFlagged)
	query := `SELECT
		COUNT(*)::int,
		COUNT(*) FILTER (WHERE UPPER(COALESCE(severity,''))='HIGH')::int,
		COUNT(*) FILTER (WHERE UPPER(COALESCE(severity,''))='MEDIUM')::int,
		COUNT(*) FILTER (WHERE UPPER(COALESCE(severity,''))='LOW')::int,
		COUNT(DISTINCT (client_id, COALESCE(platform,''), COALESCE(repository,'')))::int
	FROM (` + findingsUnion(resolved.QSchema()) + `) f` + clause
	var out models.FindingsSummary
	if err := resolved.DB.QueryRowContext(ctx, query, args...).Scan(&out.TotalFindings, &out.HighCount, &out.MediumCount, &out.LowCount, &out.AffectedRepositories); err != nil {
		return models.FindingsSummary{}, fmt.Errorf("summarize credential exposure findings: %w", err)
	}
	return out, nil
}

func scanFinding(row interface{ Scan(...any) error }) (models.Finding, error) {
	var f models.Finding
	var codeURI, githubURI, preview, confidence, category, committer, message, authoredAt sql.NullString
	var visibility, pushedAt, mlDecision, mlVersion, analystDecision, analystAt, analystBy sql.NullString
	var filesChanged, additions, deletions, stars, forks sql.NullInt64
	var mlScore sql.NullFloat64
	err := row.Scan(
		&f.FindingID, &f.ClientID, &f.Repository, &f.Branch, &f.CommitID, &f.Author, &f.CreatedAt,
		&f.Detector, &f.Severity, &f.FilePath, &f.LineNumber, &f.Redacted, &f.VerificationStatus,
		&f.RiskTier, &f.RecommendedAction, &codeURI, &f.Platform, &githubURI, &preview, &confidence,
		&category, &committer, &message, &authoredAt, &filesChanged, &additions, &deletions,
		&visibility, &stars, &forks, &pushedAt, &mlScore, &mlDecision, &mlVersion,
		&analystDecision, &analystAt, &analystBy,
	)
	if err != nil {
		return f, err
	}
	f.CodeCommitURI, f.GitHubURI, f.LinePreview = stringPtr(codeURI), stringPtr(githubURI), stringPtr(preview)
	f.Confidence, f.Category, f.Committer = stringPtr(confidence), stringPtr(category), stringPtr(committer)
	f.CommitMessage, f.CommitAuthoredAt = stringPtr(message), stringPtr(authoredAt)
	f.RepoVisibility, f.RepoPushedAt = stringPtr(visibility), stringPtr(pushedAt)
	f.MLDecision, f.MLModelVersion = stringPtr(mlDecision), stringPtr(mlVersion)
	f.AnalystDecision, f.AnalystDecidedAt, f.AnalystDecidedBy = stringPtr(analystDecision), scannerTimePtr(stringPtr(analystAt)), stringPtr(analystBy)
	f.CreatedAt = scannerTime(f.CreatedAt)
	f.FilesChanged, f.Additions, f.Deletions = nullableIntPtr(filesChanged), nullableIntPtr(additions), nullableIntPtr(deletions)
	f.RepoStars, f.RepoForks, f.MLScore = nullableIntPtr(stars), nullableIntPtr(forks), floatPtr(mlScore)
	return f, nil
}

func findingsWhere(clientID, platform string, mlFlagged bool) (string, []any) {
	where := make([]string, 0, 3)
	args := make([]any, 0, 2)
	if clientID != "" {
		args = append(args, clientID)
		where = append(where, fmt.Sprintf("client_id=$%d", len(args)))
	}
	if platform != "" {
		args = append(args, platform)
		where = append(where, fmt.Sprintf("LOWER(platform)=LOWER($%d)", len(args)))
	}
	if mlFlagged {
		where = append(where, "ml_decision='FALSE_POSITIVE'")
	} else {
		where = append(where, "ml_decision IS DISTINCT FROM 'FALSE_POSITIVE'")
	}
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}
