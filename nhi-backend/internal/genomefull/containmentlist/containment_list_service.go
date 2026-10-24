package containmentlist

// Service implements GET /api/v1/genome/containment. The current pipeline
// persists no containment actions (no table, no worker), so the list is
// always empty and flagged unavailable.
type Service struct{}

// NewService creates the service.
func NewService() *Service { return &Service{} }

// List returns the unavailable response. No store is touched.
func (s *Service) List() *Response {
	return &Response{
		Items:     []any{},
		Available: false,
		Message:   "Containment actions are not persisted by the current pipeline",
	}
}
