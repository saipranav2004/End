// Package identitypeers serves
// GET /api/v1/genome/identities/{source_type}/{identity_name}/peers: the
// identities that share the requested identity's MVP peer key (source_type
// + account_id + region + service_name + cluster_name + role_type),
// excluding the identity itself.
package identitypeers

// Response is the data block.
type Response struct {
	Peers []Peer `json:"peers"`
}

// Peer is one peer row. RiskScore is the latest
// identity_risk_scores.risk_score (null when unscored), OpenAnomalyCount
// the grouped count of open alerts, RecentCallCount the grouped
// uniqExact(log_events.event_id) in the requested window (null while
// ClickHouse is unavailable) and OutlierDimension the alerts.dimension
// with the most open alerts for the peer (null when it has none).
type Peer struct {
	IdentityName     string  `json:"identity_name"`
	DisplayName      string  `json:"display_name"`
	RiskScore        *int    `json:"risk_score"`
	OpenAnomalyCount *int    `json:"open_anomaly_count"`
	RecentCallCount  *uint64 `json:"recent_call_count"`
	OutlierDimension *string `json:"outlier_dimension"`
}
