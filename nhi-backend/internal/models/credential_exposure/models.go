package credential_exposure

// Finding is a normalized row from either scanner findings table.
type Finding struct {
	FindingID          string   `json:"finding_id"`
	ClientID           string   `json:"client_id"`
	Repository         string   `json:"repository"`
	Branch             string   `json:"branch"`
	CommitID           string   `json:"commit_id"`
	Author             string   `json:"author"`
	CreatedAt          string   `json:"created_at"`
	Detector           string   `json:"detector"`
	Severity           string   `json:"severity"`
	FilePath           string   `json:"file_path"`
	LineNumber         int      `json:"line_number"`
	Redacted           string   `json:"redacted"`
	VerificationStatus string   `json:"verification_status"`
	RiskTier           string   `json:"risk_tier"`
	RecommendedAction  string   `json:"recommended_action"`
	CodeCommitURI      *string  `json:"codecommit_uri"`
	Platform           string   `json:"platform"`
	GitHubURI          *string  `json:"github_uri"`
	LinePreview        *string  `json:"line_preview"`
	Confidence         *string  `json:"confidence"`
	Category           *string  `json:"category"`
	Committer          *string  `json:"committer"`
	CommitMessage      *string  `json:"commit_message"`
	CommitAuthoredAt   *string  `json:"commit_authored_at"`
	FilesChanged       *int     `json:"files_changed"`
	Additions          *int     `json:"additions"`
	Deletions          *int     `json:"deletions"`
	RepoVisibility     *string  `json:"repo_visibility"`
	RepoStars          *int     `json:"repo_stars"`
	RepoForks          *int     `json:"repo_forks"`
	RepoPushedAt       *string  `json:"repo_pushed_at"`
	MLScore            *float64 `json:"ml_score"`
	MLDecision         *string  `json:"ml_decision"`
	MLModelVersion     *string  `json:"ml_model_version"`
	AnalystDecision    *string  `json:"analyst_decision"`
	AnalystDecidedAt   *string  `json:"analyst_decided_at"`
	AnalystDecidedBy   *string  `json:"analyst_decided_by"`
	MLVetoScore        *float64 `json:"ml_veto_score"`
	MLVetoModelVersion *string  `json:"ml_veto_model_version"`
	ValueFingerprint   *string  `json:"value_fingerprint"`
	NoiseFlags         *string  `json:"noise_flags"`
}

type FindingsResponse struct {
	Count    int       `json:"count"`
	Findings []Finding `json:"findings"`
	Limit    int       `json:"limit,omitempty"`
	Offset   int       `json:"offset,omitempty"`
}

// Finding lists. Every finding that is not allowlisted is in exactly one.
const (
	ListMain     = "main"
	ListReview   = "review"
	ListRepeated = "repeated"
	ListNoise    = "noise"
	ListFlagged  = "ml_flagged"
)

type FindingsFilter struct {
	ClientID string
	Platform string
	List     string // one of the List* constants; empty means the main list
	Limit    int
	Offset   int
}

// ListCounts is how many findings sit in each list; the five add up to every
// finding that is not allowlisted.
type ListCounts struct {
	Main     int `json:"main"`
	Review   int `json:"review"`
	Repeated int `json:"repeated"`
	Noise    int `json:"noise"`
	Flagged  int `json:"ml_flagged"`
}

type FindingsSummary struct {
	TotalFindings        int `json:"total_findings"`
	HighCount            int `json:"high_count"`
	MediumCount          int `json:"medium_count"`
	LowCount             int `json:"low_count"`
	AffectedRepositories int `json:"affected_repositories"`
	// Lists counts every list for the same client/platform filter, whichever
	// list the other figures describe.
	Lists ListCounts `json:"lists"`
}

type FindingsSummaryFilter struct {
	ClientID string
	Platform string
	List     string
}

type AllowlistEntry struct {
	ClientID    string `json:"client_id"`
	FilePath    string `json:"file_path"`
	Detector    string `json:"detector"`
	Redacted    string `json:"redacted"`
	Reason      string `json:"reason"`
	DismissedBy string `json:"dismissed_by"`
	DismissedAt string `json:"dismissed_at"`
}

type AllowlistListResponse struct {
	Entries []AllowlistEntry `json:"entries"`
}

type AllowlistRequest struct {
	ClientID    string `json:"client_id"`
	FilePath    string `json:"file_path"`
	Detector    string `json:"detector"`
	Redacted    string `json:"redacted"`
	Reason      string `json:"reason,omitempty"`
	DismissedBy string `json:"dismissed_by,omitempty"`
}

type FindingDecisionRequest struct {
	FindingID string `json:"finding_id"`
	Decision  string `json:"decision"`
	DecidedBy string `json:"decided_by,omitempty"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}

type DeepScanRequest struct {
	ClientID   string `json:"client_id"`
	Repository string `json:"repository"`
	Platform   string `json:"platform,omitempty"` // "github" | "codecommit"; inferred when omitted
}

// DeepScanAccepted is the 202 body: the walk is queued (or was already).
type DeepScanAccepted struct {
	Accepted       bool   `json:"accepted"` // always true, as the scanner API answers
	Status         string `json:"status"`   // "running"
	ClientID       string `json:"client_id"`
	RepoKey        string `json:"repo_key"`
	Platform       string `json:"platform"`
	RequestedAt    string `json:"requested_at"`
	AlreadyRunning bool   `json:"already_running"`
}

type DeepScanResult struct {
	ClientID        string  `json:"client_id"`
	RepoKey         string  `json:"repo_key"`
	Platform        string  `json:"platform"`
	RequestedAt     *string `json:"requested_at"`
	CompletedAt     *string `json:"completed_at"`
	CommitsScanned  *int    `json:"commits_scanned"`
	Findings        *int    `json:"findings"`
	FailedAt        *string `json:"failed_at"`
	Error           *string `json:"error"`
	Status          string  `json:"status"`
	LastCommitID    *string `json:"last_commit_id,omitempty"`
	UpdatedAt       *string `json:"updated_at,omitempty"`
	PendingCommitID *string `json:"pending_commit_id,omitempty"`
	PendingSince    *string `json:"pending_since,omitempty"`
}
