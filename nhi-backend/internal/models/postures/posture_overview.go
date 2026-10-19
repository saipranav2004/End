package postures

// GET /posture/overview?window=30d — the whole Posture page in one call.

type PostureOverview struct {
	Fleet             FleetScore       `json:"fleet"`
	Trend             []TrendPoint     `json:"trend"`
	Distribution      BandDistribution `json:"distribution"`
	IdentitiesWith    map[string]int   `json:"identities_with"`
	QuickWins         []QuickWin       `json:"quick_wins"`
	Pillars           []PillarScore    `json:"pillars"`
	WeakestIdentities []WeakIdentity   `json:"weakest_identities"`
	ByCategory        []CategoryStat   `json:"by_category"`
	ByAccount         []AccountStat    `json:"by_account"`
}

type FleetScore struct {
	Score            *float64    `json:"score"`
	Grade            string      `json:"grade"`
	Band             string      `json:"band"`
	IdentitiesScored int         `json:"identities_scored"`
	Change           FleetChange `json:"change"`
}

type FleetChange struct {
	Window   string   `json:"window"`
	Previous *float64 `json:"previous"`
	Delta    *float64 `json:"delta"`
}

// TrendPoint.At is a timestamp for per-scan windows (24h, 3d, 7d) and a plain
// date for daily windows (30d, 60d, 90d).
type TrendPoint struct {
	At    string  `json:"at"`
	Score float64 `json:"score"`
}

type BandDistribution struct {
	Healthy  int `json:"healthy"`
	Fair     int `json:"fair"`
	Poor     int `json:"poor"`
	Critical int `json:"critical"`
}

type QuickWin struct {
	RuleID     string  `json:"rule_id"`
	Title      string  `json:"title"`
	Pillar     string  `json:"pillar"`
	Identities int     `json:"identities"`
	FleetGain  float64 `json:"fleet_gain"`
}

type WeakIdentity struct {
	IdentityID      string   `json:"identity_id"`
	Name            string   `json:"name"`
	FunctionalClass *string  `json:"functional_class"`
	Account         string   `json:"account"`
	Score           *float64 `json:"score"`
	Band            string   `json:"band"`
	ChecksFailed    int      `json:"checks_failed"`
}

type CategoryStat struct {
	FunctionalClass string   `json:"functional_class"`
	Count           int      `json:"count"`
	Mean            float64  `json:"mean"`
	Change          *float64 `json:"change"`
}

type AccountStat struct {
	Account string   `json:"account"`
	Count   int      `json:"count"`
	Mean    float64  `json:"mean"`
	Change  *float64 `json:"change"`
}
