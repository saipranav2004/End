package postures

import "time"

// PATCH /findings/{id} (analyst / admin)

// Finding statuses a caller may set.
const (
	FindingOpen       = "OPEN"
	FindingAccepted   = "ACCEPTED"
	FindingSuppressed = "SUPPRESSED"
	FindingResolved   = "RESOLVED" // set by the engine only
)

// FindingUpdateRequest is the PATCH body. Every field is optional but at least
// one must be present.
type FindingUpdateRequest struct {
	Status          *string    `json:"status"`
	AcceptedReason  *string    `json:"accepted_reason"`
	SuppressedUntil *time.Time `json:"suppressed_until"`
	AssignedTo      *string    `json:"assigned_to"`
	Note            *string    `json:"note"`
}

// FindingUpdate carries the request plus the caller. IfMatch, when set, is
// the updated_at the caller last saw (If-Match header); a finding changed
// since then is refused with ErrConflict.
type FindingUpdate struct {
	FindingID string
	Actor     string
	IfMatch   time.Time
	Request   FindingUpdateRequest
}

type FindingState struct {
	FindingID       string     `json:"finding_id"`
	Status          string     `json:"status"`
	AcceptedReason  *string    `json:"accepted_reason"`
	SuppressedUntil *time.Time `json:"suppressed_until"`
	AssignedTo      *string    `json:"assigned_to"`
	Notes           *string    `json:"notes"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
