// Package identitydrift serves the v3 identity baseline-health endpoint.
package identitydrift

// Response is the data block returned by the identity drift endpoint. A
// missing drift run leaves scalar values null and dimensions/reasons empty.
type Response struct {
	Status                 *string     `json:"status"`
	Drift                  *float64    `json:"drift"`
	RawDrift               *float64    `json:"raw_drift"`
	ModelConfidencePercent *float64    `json:"model_confidence_percent"`
	VerdictTrust           *float64    `json:"verdict_trust"`
	RetrainingStatus       *string     `json:"retraining_status"`
	PSIMax                 *float64    `json:"psi_max"`
	PSIMaxDimension        *string     `json:"psi_max_dimension"`
	Reasons                []string    `json:"reasons"`
	Dimensions             []Dimension `json:"dimensions"`
}

// Dimension is the contract-safe PSI/JSD/G-test evidence for one dimension.
type Dimension struct {
	Dimension    string   `json:"dimension"`
	PSI          *float64 `json:"psi"`
	JSD          *float64 `json:"jsd"`
	GTest        *float64 `json:"g_test"`
	Contribution *float64 `json:"contribution"`
}
