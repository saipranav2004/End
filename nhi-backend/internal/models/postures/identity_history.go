package postures

// GET /identities/{id}/history?window=30d — the History tab.

type IdentityHistoryPoint struct {
	At           string   `json:"at"`
	Score        *float64 `json:"score"`
	Band         string   `json:"band"`
	ChecksFailed int      `json:"checks_failed"`
}
