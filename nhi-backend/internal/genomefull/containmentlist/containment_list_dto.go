// Package containmentlist serves GET /api/v1/genome/containment. No
// containment action is persisted by the current pipeline, so the list is
// always empty and flagged unavailable.
package containmentlist

// Response is the data block.
type Response struct {
	Items     []any  `json:"items"`
	Available bool   `json:"available"`
	Message   string `json:"message"`
}
