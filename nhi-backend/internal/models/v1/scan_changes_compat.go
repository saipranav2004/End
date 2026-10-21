package v1

import (
	"encoding/json"
	"time"
)

// The reference API contract (the shared Postman collection) names some of
// these fields differently. Both names are sent, so clients written against
// either keep working; the console reads ours.

func (e CloudTrailEvidence) MarshalJSON() ([]byte, error) {
	type plain CloudTrailEvidence
	return json.Marshal(struct {
		plain
		CreatedBy string    `json:"created_by"` // = caller_arn
		CreatedAt time.Time `json:"created_at"` // = event_time
	}{plain(e), e.CallerARN, e.EventTime})
}

func (c ScanChange) MarshalJSON() ([]byte, error) {
	type plain ScanChange
	entityID := c.IdentityID
	if c.EntityType == EntityCredential {
		entityID = c.CredentialID
	}
	return json.Marshal(struct {
		plain
		ID       string     `json:"id"`        // = change_id
		EntityID *string    `json:"entity_id"` // the row the change belongs to
		ScanTime *time.Time `json:"scan_time"` // = scan_at
	}{plain(c), c.ChangeID, entityID, c.ScanAt})
}

func (r RecentChanges) MarshalJSON() ([]byte, error) {
	type plain RecentChanges
	return json.Marshal(struct {
		plain
		LastScanTime *time.Time `json:"last_scan_time"` // = last_scan_at
	}{plain(r), r.LastScanAt})
}

func (s ScanChangeSummary) MarshalJSON() ([]byte, error) {
	type plain ScanChangeSummary
	return json.Marshal(struct {
		plain
		Created  int            `json:"created"`
		Deleted  int            `json:"deleted"`
		Modified int            `json:"modified"`
		ByEntity map[string]int `json:"by_entity"` // = by_entity_type
	}{plain(s), s.ByChangeType[ChangeCreated], s.ByChangeType[ChangeDeleted], s.ByChangeType[ChangeModified], s.ByEntityType})
}

func (r ScanChangesResponse) MarshalJSON() ([]byte, error) {
	type plain ScanChangesResponse
	return json.Marshal(struct {
		plain
		ScanID         string     `json:"scan_id"`          // = scan.scan_id
		Status         string     `json:"status"`           // = scan.status
		ScanTime       *time.Time `json:"scan_time"`        // = scan.completed_at
		PreviousScanID *string    `json:"previous_scan_id"` // = scan.previous_scan_id
	}{plain(r), r.Scan.ScanID, r.Scan.Status, r.Scan.CompletedAt, r.Scan.PreviousScanID})
}
