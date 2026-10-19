package postures

// GET /posture/export?format=csv&window=30d&<identity filters>
//
// The CSV is streamed in chunks; ExportRow is one line of it.

// ExportChunkSize is the number of rows read per query while streaming.
const ExportChunkSize = 1000

type ExportRow struct {
	IdentityID        string
	Name              string
	Type              string
	FunctionalClass   string
	Account           string
	Score             *float64
	Band              string
	Grade             string
	ChecksFailed      int
	FailedCritical    int
	FailedHigh        int
	FailedMedium      int
	FailedLow         int
	WeakestPillar     string
	CredentialType    string
	CredentialAgeDays *int
	Change            *float64
	OpenAlerts        int
}

// ExportColumns returns the CSV header for a window.
func ExportColumns(w Window) []string {
	return []string{
		"identity_id", "name", "type", "functional_class", "account", "score", "band", "grade",
		"checks_failed", "failed_critical", "failed_high", "failed_medium", "failed_low",
		"weakest_pillar", "credential_type", "credential_age_days", "change_" + w.Label, "open_alerts",
	}
}
