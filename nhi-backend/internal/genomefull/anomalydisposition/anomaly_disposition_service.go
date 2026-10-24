package anomalydisposition

// Service implements POST /api/v1/genome/anomalies/{alert_id}/disposition.
// The current pipeline has no disposition history table — alerts.status
// is a single basic value and alert_feedback is reviewer feedback, not
// workflow state — so the endpoint reports the feature unavailable and
// writes nothing.
type Service struct{}

// NewService creates the service.
func NewService() *Service { return &Service{} }

// Record returns the unavailable response. No store is touched.
func (s *Service) Record() *Response {
	return &Response{
		Available: false,
		Reason:    "NEEDS TO BE ADDED: no disposition history table",
	}
}
