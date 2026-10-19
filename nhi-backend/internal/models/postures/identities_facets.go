package postures

// GET /identities/facets?<same filters>

type IdentityFacets struct {
	Total    int                     `json:"total"`
	Band     map[string]int          `json:"band"`
	Severity map[string]int          `json:"severity"`
	Category map[string]int          `json:"category"`
	Account  map[string]AccountFacet `json:"account"`
	Type     map[string]int          `json:"type"`
	Pillar   map[string]int          `json:"pillar"`
	Changed  map[string]int          `json:"changed"`
}

type AccountFacet struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
