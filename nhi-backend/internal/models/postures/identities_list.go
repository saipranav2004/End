package postures

// GET /identities — the identity table (and the filters shared by
// /identities/facets and /posture/export).

// Sort keys accepted by ?sort=.
const (
	SortScore     = "score"  // lowest score first (default)
	SortScoreDesc = "-score" // highest score first
	SortFailed    = "failed" // most failed checks first
	SortChange    = "change" // biggest drop in the window first
	SortName      = "name"
)

var SortKeys = []string{SortScore, SortScoreDesc, SortFailed, SortChange, SortName}

// Values of ?changed=.
const (
	ChangedDown = "down"
	ChangedUp   = "up"
	ChangedSame = "same"
)

// IdentityFilter holds the parsed and validated query parameters.
type IdentityFilter struct {
	Window        Window
	Bands         []string // HEALTHY, FAIR, POOR, CRITICAL
	Severities    []string // Critical, High, Medium, Low
	Categories    []string // functional_class
	Accounts      []string // account id
	Types         []string // identity type
	Pillars       []string // weakest pillar
	FailingChecks []string // rule ids
	Changed       string   // down | up | same
	Q             string   // name / ARN search
	Sort          string
	Limit         int
	Cursor        string
}

type IdentityListItem struct {
	IdentityID      string           `json:"identity_id"`
	Name            string           `json:"name"`
	ARN             string           `json:"arn"`
	Type            string           `json:"type"`
	FunctionalClass *string          `json:"functional_class"`
	Account         AccountRef       `json:"account"`
	Score           *float64         `json:"score"`
	Band            string           `json:"band"`
	Grade           string           `json:"grade"`
	WeakestPillar   *string          `json:"weakest_pillar"`
	Failed          FailedCounts     `json:"failed"`
	Credential      *CredentialBrief `json:"credential"`
	Change          Change           `json:"change"`
}

type CredentialBrief struct {
	Type    string `json:"type"`
	AgeDays *int   `json:"age_days"`
}
