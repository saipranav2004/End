package v1

import (
	"encoding/json"
	"time"
)

// Scan change tracking, read from the scanner's nhi_scans_aws and
// scan_changes tables.

// Change and entity types, as stored.
const (
	ChangeCreated  = "CREATED"
	ChangeDeleted  = "DELETED"
	ChangeModified = "MODIFIED"

	EntityIdentity   = "identity"
	EntityCredential = "credential"
	EntityPermission = "permission"
	EntityBinding    = "binding"
	EntityTrustEdge  = "trust_edge"
)

// ChangeTypes and EntityTypes list the valid values, in display order.
var (
	ChangeTypes = []string{ChangeCreated, ChangeDeleted, ChangeModified}
	EntityTypes = []string{EntityIdentity, EntityCredential, EntityPermission, EntityBinding, EntityTrustEdge}
)

// ScanInfo describes one completed discovery run.
type ScanInfo struct {
	ScanID         string     `json:"scan_id"`
	Status         string     `json:"status"`
	AccountID      *string    `json:"account_id"` // null: every connected account
	StartedAt      time.Time  `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	PreviousScanID *string    `json:"previous_scan_id"` // null: first scan
	PreviousScanAt *time.Time `json:"previous_scan_at"`
}

// ScanChangeHighlight is one line of the Overview card, e.g.
// {"CREATED", "identity", 5, "5 new identities discovered"}.
type ScanChangeHighlight struct {
	ChangeType string `json:"change_type"`
	EntityType string `json:"entity_type"`
	Count      int    `json:"count"`
	Label      string `json:"label"`
}

// RecentChanges is the dashboard overview's `recent_changes`: what the latest
// completed scan found, within the caller's scope. Null before the first scan.
type RecentChanges struct {
	LastScanID     string                `json:"last_scan_id"`
	LastScanAt     *time.Time            `json:"last_scan_at"`
	PreviousScanID *string               `json:"previous_scan_id"`
	PreviousScanAt *time.Time            `json:"previous_scan_at"`
	TotalChanges   int                   `json:"total_changes"`
	ByChangeType   map[string]int        `json:"by_change_type"`
	Highlights     []ScanChangeHighlight `json:"highlights"`
}

// ScanChangeSummary counts a scan's changes within scope, before the list
// filters, so the totals stay put while the list is filtered.
type ScanChangeSummary struct {
	Total        int            `json:"total"`
	ByChangeType map[string]int `json:"by_change_type"`
	ByEntityType map[string]int `json:"by_entity_type"`
}

// CloudTrailEvidence is the CloudTrail event that created or deleted the
// entity, when one is found.
type CloudTrailEvidence struct {
	CallerARN string    `json:"caller_arn"`
	EventName string    `json:"event_name"`
	SourceIP  string    `json:"source_ip"`
	EventTime time.Time `json:"event_time"`
}

// ScanChange is one change.
type ScanChange struct {
	ChangeID     string          `json:"change_id"`
	ScanID       string          `json:"scan_id"`
	ScanAt       *time.Time      `json:"scan_at"`
	ChangeType   string          `json:"change_type"`
	EntityType   string          `json:"entity_type"`
	EntityARN    string          `json:"entity_arn"`
	EntityName   string          `json:"entity_name"`
	IdentityID   *string         `json:"identity_id"`
	CredentialID *string         `json:"credential_id"`
	AccountID    string          `json:"account_id"`
	Region       string          `json:"region"`
	Summary      string          `json:"summary"`
	Details      json.RawMessage `json:"details"`
	DetectedAt   time.Time       `json:"detected_at"`
	// Only on CREATED / DELETED identity and credential changes in the
	// Changes tabs, when CloudTrail recorded the call.
	CloudTrailEvidence *CloudTrailEvidence `json:"cloudtrail_evidence,omitempty"`
}

// ScanChangesFilter is the query of GET /scans/{scan_id}/changes.
type ScanChangesFilter struct {
	ScanID     string
	ChangeType string
	EntityType string
	Page       int
	Limit      int
}

type ScanChangesPagination struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// ScanChangesResponse is GET /api/v1/scans/{scan_id}/changes.
type ScanChangesResponse struct {
	Scan       ScanInfo              `json:"scan"`
	Summary    ScanChangeSummary     `json:"summary"`
	Changes    []ScanChange          `json:"changes"`
	Pagination ScanChangesPagination `json:"pagination"`
}

// ScanListItem is one row of GET /api/v1/scans.
type ScanListItem struct {
	ScanInfo
	TotalChanges int `json:"total_changes"`
}

type ScanListResponse struct {
	Scans []ScanListItem `json:"scans"`
}

// EntityChangesResponse is tab=changes of an identity or a credential:
// newest first, at most EntityChangesLimit rows.
type EntityChangesResponse struct {
	Tab string `json:"tab"`
	// The entity's ARN, as the reference contract names it per tab.
	IdentityARN   string       `json:"identity_arn,omitempty"`
	CredentialARN string       `json:"credential_arn,omitempty"`
	Changes       []ScanChange `json:"changes"`
	// Truncated: older changes exist beyond the limit.
	Truncated bool `json:"truncated"`
	// ChangeTrackingAvailable is false until the change tables exist.
	ChangeTrackingAvailable bool `json:"change_tracking_available"`
}

// EntityChangesLimit caps the Changes tabs.
const EntityChangesLimit = 50

// ScanStatus is the data of GET /scans/{scan_id}/status.
type ScanStatus struct {
	ScanID   string     `json:"scan_id"`
	Status   string     `json:"status"`
	ScanTime *time.Time `json:"scan_time"`
}
