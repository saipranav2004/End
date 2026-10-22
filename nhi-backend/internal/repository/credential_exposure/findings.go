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
 ml_decision, ml_model_version, analyst_decision, analyst_decided_at, analyst_decided_by,
 ml_veto_score, ml_veto_model_version, value_fingerprint, noise_flags`

// platformSelect reads one scanner table minus what a person marked safe. The
// tables live in the tenant schema (passed in already quoted) and are isolated
// by schema, not by account_id; client_id filtering still serves multi-client tenants.
func platformSelect(schema, table, fallbackPlatform string) string {
	return ` SELECT finding_id,client_id,repository,branch,commit_id,author,created_at,detector,severity,file_path,line_number,redacted,verification_status,risk_tier,recommended_action,codecommit_uri,COALESCE(NULLIF(platform,''),'` + fallbackPlatform + `') AS platform,github_uri,line_preview,confidence,category,committer,commit_message,commit_authored_at,files_changed,additions,deletions,repo_visibility,repo_stars,repo_forks,repo_pushed_at,ml_score,ml_decision,ml_model_version,analyst_decision,analyst_decided_at,analyst_decided_by,ml_veto_score,ml_veto_model_version,value_fingerprint,noise_flags
 FROM ` + schema + `.` + table + ` f
 WHERE NOT EXISTS (SELECT 1 FROM ` + schema + `.secscan_allowlist_dashboard a WHERE a.client_id=f.client_id AND a.file_path=f.file_path AND a.detector=f.detector AND a.redacted=f.redacted)`
}

// genericDetector: detectors with no provider key format, the only ones the
// second model may take out of the main list.
const genericDetector = `(b.detector ~ '^(generic_|high_entropy_string|jwt|basic_auth_header|bearer_token|authorization_header|url_embedded|curl_|docker_login|webhook_url_generic|azure_sas_token)'
   OR (b.detector ~ '_connection_string$' AND b.detector !~ '^azure_storage_'))`

// listCase sorts a finding into exactly one list, each rule seeing only what
// the earlier rules left. A missing score or tag means "not scored", never
// zero, so NULL falls through to the main list instead of hiding a finding.
const listCase = `CASE
 WHEN COALESCE(b.ml_model_version,'')='rule:private-key-with-body' THEN 'main'
 WHEN r.value_fingerprint IS NOT NULL THEN 'repeated'
 WHEN string_to_array(regexp_replace(COALESCE(b.noise_flags,''),'\s+','','g'),',') && ARRAY['known_example_value','expired_signed_url','vector_image_file','integrity_hash','arn_identifier','password_hash','plain_url']::text[] THEN 'noise'
 WHEN (b.ml_decision='FALSE_POSITIVE' AND b.ml_score>=0.25)
   OR (b.ml_decision='REAL_SECRET' AND (b.ml_score<0.40 OR (` + genericDetector + ` AND b.ml_veto_score<0.50))) THEN 'review'
 WHEN b.ml_decision='FALSE_POSITIVE'
   OR (` + genericDetector + ` AND b.ml_veto_score<0.13305)
   OR (b.detector ~ '_broad$' AND NOT COALESCE(b.ml_score>=0.95 AND b.ml_veto_score>=0.30, false)) THEN 'ml_flagged'
 ELSE 'main' END`

// findingsUnion is every finding that is not allowlisted, from both platform
// tables, with the list each one belongs to as finding_list. A value repeated
// at three or more different places in one commit is "repeated"; private-key
// detectors are exempt because their value is always the same header.
func findingsUnion(schema string) string {
	return `WITH base AS (` + platformSelect(schema, "secscan_findings_codecommit", "codecommit") + `
 UNION ALL` + platformSelect(schema, "secscan_findings_github", "github") + `),
repeated AS (
 SELECT platform, client_id, repository, commit_id, value_fingerprint FROM base
 WHERE COALESCE(value_fingerprint,'')<>'' AND detector !~* 'private_key'
 GROUP BY platform, client_id, repository, commit_id, value_fingerprint
 HAVING COUNT(DISTINCT (file_path, line_number)) >= 3)
SELECT b.*, ` + listCase + ` AS finding_list
FROM base b LEFT JOIN repeated r
 ON r.platform=b.platform AND r.client_id IS NOT DISTINCT FROM b.client_id
 AND r.repository IS NOT DISTINCT FROM b.repository AND r.commit_id IS NOT DISTINCT FROM b.commit_id
 AND r.value_fingerprint=b.value_fingerprint`
}

func (r *FindingsRepository) List(ctx context.Context, resolved *scope.DataScope, filter models.FindingsFilter) ([]models.Finding, int, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, 0, errScopeRequired
	}
	clause, args := findingsWhere(filter.ClientID, filter.Platform, listOrMain(filter.List))
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
	// The severity and repository figures describe one list; the list counts
	// describe all five, for the same client and platform.
	clause, args := findingsWhere(filter.ClientID, filter.Platform, "")
	args = append(args, listOrMain(filter.List))
	pick := fmt.Sprintf("finding_list=$%d", len(args))
	query := `SELECT
		COUNT(*) FILTER (WHERE ` + pick + `)::int,
		COUNT(*) FILTER (WHERE ` + pick + ` AND UPPER(COALESCE(severity,''))='HIGH')::int,
		COUNT(*) FILTER (WHERE ` + pick + ` AND UPPER(COALESCE(severity,''))='MEDIUM')::int,
		COUNT(*) FILTER (WHERE ` + pick + ` AND UPPER(COALESCE(severity,''))='LOW')::int,
		COUNT(DISTINCT (client_id, COALESCE(platform,''), COALESCE(repository,''))) FILTER (WHERE ` + pick + `)::int,
		COUNT(*) FILTER (WHERE finding_list='main')::int,
		COUNT(*) FILTER (WHERE finding_list='review')::int,
		COUNT(*) FILTER (WHERE finding_list='repeated')::int,
		COUNT(*) FILTER (WHERE finding_list='noise')::int,
		COUNT(*) FILTER (WHERE finding_list='ml_flagged')::int
	FROM (` + findingsUnion(resolved.QSchema()) + `) f` + clause
	var out models.FindingsSummary
	l := &out.Lists
	if err := resolved.DB.QueryRowContext(ctx, query, args...).Scan(&out.TotalFindings, &out.HighCount, &out.MediumCount, &out.LowCount, &out.AffectedRepositories,
		&l.Main, &l.Review, &l.Repeated, &l.Noise, &l.Flagged); err != nil {
		return models.FindingsSummary{}, fmt.Errorf("summarize credential exposure findings: %w", err)
	}
	return out, nil
}

func scanFinding(row interface{ Scan(...any) error }) (models.Finding, error) {
	var f models.Finding
	var codeURI, githubURI, preview, confidence, category, committer, message, authoredAt sql.NullString
	var visibility, pushedAt, mlDecision, mlVersion, analystDecision, analystAt, analystBy sql.NullString
	var filesChanged, additions, deletions, stars, forks sql.NullInt64
	var mlScore, vetoScore sql.NullFloat64
	var vetoVersion, fingerprint, noiseFlags sql.NullString
	err := row.Scan(
		&f.FindingID, &f.ClientID, &f.Repository, &f.Branch, &f.CommitID, &f.Author, &f.CreatedAt,
		&f.Detector, &f.Severity, &f.FilePath, &f.LineNumber, &f.Redacted, &f.VerificationStatus,
		&f.RiskTier, &f.RecommendedAction, &codeURI, &f.Platform, &githubURI, &preview, &confidence,
		&category, &committer, &message, &authoredAt, &filesChanged, &additions, &deletions,
		&visibility, &stars, &forks, &pushedAt, &mlScore, &mlDecision, &mlVersion,
		&analystDecision, &analystAt, &analystBy, &vetoScore, &vetoVersion, &fingerprint, &noiseFlags,
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
	f.MLVetoScore, f.MLVetoModelVersion = floatPtr(vetoScore), stringPtr(vetoVersion)
	f.ValueFingerprint, f.NoiseFlags = stringPtr(fingerprint), stringPtr(noiseFlags)
	return f, nil
}

// listOrMain names the list to read; no list means the main one.
func listOrMain(list string) string {
	if list == "" {
		return models.ListMain
	}
	return list
}

// findingsWhere filters by client and platform and, when list is not empty, by list.
func findingsWhere(clientID, platform, list string) (string, []any) {
	where := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if clientID != "" {
		args = append(args, clientID)
		where = append(where, fmt.Sprintf("client_id=$%d", len(args)))
	}
	if platform != "" {
		args = append(args, platform)
		where = append(where, fmt.Sprintf("LOWER(platform)=LOWER($%d)", len(args)))
	}
	if list != "" {
		args = append(args, list)
		where = append(where, fmt.Sprintf("finding_list=$%d", len(args)))
	}
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}
