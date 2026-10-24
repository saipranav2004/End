package common

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"nhi-backend-api/internal/workloadtypes"
	"sort"
	"strconv"
	"strings"
	"time"

	"nhi-backend-api/internal/scope"
)

// Shared by the anomaly endpoints (anomalylist, anomalydetail,
// anomalyevidence, anomalyfeedback): the alerts/alert_feedback row shapes
// and scans, and how an alerts row and its log_events row are turned into
// response fields. Nothing here invents data — every value is a column of
// one of the two stores, or null when that column cannot be read.

// ErrAnomalyNotFound is returned when no alerts row has the requested id in
// the caller's tenant.
var ErrAnomalyNotFound = errors.New("anomaly not found")

// ---------------------------------------------------------------------------
// alerts
// ---------------------------------------------------------------------------

// AlertRow is one alerts row — every column the anomaly endpoints use.
// BaselineValue, ObservedValue and IdentityMetadata are the stored JSONB
// documents, unparsed.
type AlertRow struct {
	ID               int64
	Key              MonitoringKey // (source_type, identity_name)
	EventID          string
	RequestID        *string
	Dimension        string
	Score            float64
	Reason           string
	BaselineValue    []byte
	ObservedValue    []byte
	IdentityMetadata []byte
	WindowStart      time.Time
	WindowEnd        time.Time
	Status           string
	CreatedAt        time.Time
}

// AlertColumns is the select list shared by the alert readers (alias "a").
const AlertColumns = `a.id, a.source_type, a.identity_name, a.event_id, a.request_id, a.dimension, a.score, a.reason,
	       a.baseline_value, a.observed_value, a.identity_metadata, a.window_start, a.window_end, a.status, a.created_at`

// ScanAlert reads one AlertColumns row.
func ScanAlert(sc RowScanner) (AlertRow, error) {
	var (
		row       AlertRow
		requestID sql.NullString
	)
	if err := sc.Scan(&row.ID, &row.Key.SourceType, &row.Key.IdentityName, &row.EventID, &requestID, &row.Dimension, &row.Score, &row.Reason,
		&row.BaselineValue, &row.ObservedValue, &row.IdentityMetadata, &row.WindowStart, &row.WindowEnd, &row.Status, &row.CreatedAt); err != nil {
		return AlertRow{}, err
	}
	if requestID.Valid {
		v := requestID.String
		row.RequestID = &v
	}
	row.WindowStart, row.WindowEnd, row.CreatedAt = row.WindowStart.UTC(), row.WindowEnd.UTC(), row.CreatedAt.UTC()
	return row, nil
}

// AlertCursor is the keyset position of a page: rows strictly after
// (CreatedAt, ID) in the DESC ordering are returned.
type AlertCursor struct {
	CreatedAt time.Time
	ID        int64
}

// AlertByID reads the alerts row with the given primary key, or nil when
// there is none in this tenant. Shared by the detail, evidence and feedback
// repositories.
func AlertByID(ctx context.Context, resolved *scope.DataScope, alertID int64) (*AlertRow, error) {
	q := `SELECT ` + AlertColumns + ` FROM ` + resolved.Table("alerts") + ` a WHERE a.id = $1`
	row, err := ScanAlert(resolved.DB.QueryRowContext(ctx, q, alertID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, WrapMonitoringErr(ctx, "alert by id", err)
	}
	if !workloadtypes.Allows(row.Key.SourceType) {
		return nil, nil // a workload kind NHI Genome is set not to show
	}
	return &row, nil
}

// ---------------------------------------------------------------------------
// alert_feedback
// ---------------------------------------------------------------------------

// FeedbackRow is the response projection of one alert_feedback row. The
// production row is keyed by (account_id, source_type, event_id); account_id
// is used to select the row but is not part of the feedback response.
type FeedbackRow struct {
	Key                 MonitoringKey // source_type + event_id (IdentityName carries the event_id)
	Decision            string
	ApprovedForBaseline bool
	Reviewer            *string
	Reason              *string
	BaselineVersion     *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// FeedbackColumns is the select list shared by the feedback readers (alias "f").
const FeedbackColumns = `f.source_type, f.event_id, f.decision, f.approved_for_baseline, f.reviewer, f.reason, f.baseline_version, f.created_at, f.updated_at`

// ScanFeedback reads one FeedbackColumns row.
func ScanFeedback(sc RowScanner) (FeedbackRow, error) {
	var (
		row                       FeedbackRow
		reviewer, reason, version sql.NullString
	)
	if err := sc.Scan(&row.Key.SourceType, &row.Key.IdentityName, &row.Decision, &row.ApprovedForBaseline, &reviewer, &reason, &version, &row.CreatedAt, &row.UpdatedAt); err != nil {
		return FeedbackRow{}, err
	}
	row.Reviewer, row.Reason, row.BaselineVersion = NullString(reviewer), NullString(reason), NullString(version)
	row.CreatedAt, row.UpdatedAt = row.CreatedAt.UTC(), row.UpdatedAt.UTC()
	return row, nil
}

// NullString converts a sql.NullString to *string.
func NullString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// Feedback is the reviewer-feedback block shared by the anomaly evidence
// and feedback responses. The underlying alert_feedback row is selected by
// (account_id, source_type, event_id); account_id is not returned in this block.
// It is reviewer feedback, not workflow state.
type Feedback struct {
	Decision            string  `json:"decision"`
	Reviewer            *string `json:"reviewer"`
	Reason              *string `json:"reason"`
	ApprovedForBaseline bool    `json:"approved_for_baseline"`
}

// ---------------------------------------------------------------------------
// Rendering helpers shared by the anomaly list/detail/evidence responses
// ---------------------------------------------------------------------------

// AnomalyIdentityRef is the identity block of the anomaly endpoints.
type AnomalyIdentityRef struct {
	SourceType   string `json:"source_type"`
	IdentityName string `json:"identity_name"`
	DisplayName  string `json:"display_name"`
}

// AnomalyIdentityRefFor is the identity block of an alert: the key stored
// on the alerts row with the display name of the discovered identity
// (matched by identity key, then workload key); when the key is unknown to
// discovery the display_name falls back to alerts.identity_metadata.display_name
// and finally to the identity_name itself.
func AnomalyIdentityRefFor(a AlertRow, set *IdentityIndex) AnomalyIdentityRef {
	ref := AnomalyIdentityRef{SourceType: a.Key.SourceType, IdentityName: a.Key.IdentityName}
	if i, ok := set.IndexOf(a.Key); ok && set.IDs[i].DisplayName != "" {
		ref.DisplayName = set.IDs[i].DisplayName
		return ref
	}
	if v := identityMetadataString(a.IdentityMetadata, "display_name"); v != "" {
		ref.DisplayName = v
		return ref
	}
	ref.DisplayName = a.Key.IdentityName
	return ref
}

// identityMetadataString reads one string field of alerts.identity_metadata.
func identityMetadataString(metadata []byte, field string) string {
	var m map[string]any
	if len(metadata) == 0 || json.Unmarshal(metadata, &m) != nil {
		return ""
	}
	if s, ok := m[field].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// AnomalyRegion is region: log_events.region of the alert's event, falling
// back to alerts.identity_metadata.region; null when neither exists.
func AnomalyRegion(a AlertRow, ev *AlertEventRow) *string {
	if ev != nil && strings.TrimSpace(ev.Region) != "" {
		return OptionalString(ev.Region)
	}
	return OptionalString(identityMetadataString(a.IdentityMetadata, "region"))
}

// AnomalyAccountID is account_id: log_events.account_id of the alert's event,
// falling back to alerts.identity_metadata.account_id; nil when neither exists.
func AnomalyAccountID(a AlertRow, ev *AlertEventRow) *string {
	if ev != nil && strings.TrimSpace(ev.AccountID) != "" {
		return OptionalString(ev.AccountID)
	}
	return OptionalString(identityMetadataString(a.IdentityMetadata, "account_id"))
}

// AnomalyProvenance is log_events.provenance_confidence; null without the event.
func AnomalyProvenance(ev *AlertEventRow) *int {
	if ev == nil {
		return nil
	}
	return IntPtr(int(ev.ProvenanceConfidence))
}

// AnomalySeverity is log_events.event_risk_severity exactly as stored
// (Low/Medium/High/Critical once scored, Pending/Unknown before); null
// without the event. No severity is persisted on the alerts row.
func AnomalySeverity(ev *AlertEventRow) *string {
	if ev == nil {
		return nil
	}
	return OptionalString(ev.EventRiskSeverity)
}

// WhatChangedText renders the alerts row as short text: the observed value
// "was observed outside the stored baseline"; when the observed_value
// document has no printable content the dimension and the pipeline's
// reason are used instead.
func WhatChangedText(a AlertRow) string {
	if observed := SummarizeJSONValue(a.ObservedValue); observed != "" {
		return observed + " was observed outside the stored baseline"
	}
	return a.Dimension + ": " + a.Reason
}

// SummarizeJSONValue renders a stored JSON value as short text: scalars as
// themselves, arrays as a comma list, objects as "key: value" pairs sorted
// by key (a single "value" key is unwrapped). Nested documents beyond one
// level are rendered as compact JSON. "" when there is nothing to show.
func SummarizeJSONValue(raw []byte) string {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return renderJSONValue(v, 0)
}

func renderJSONValue(v any, depth int) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s := renderJSONValue(item, depth+1); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		if len(t) == 1 {
			if inner, ok := t["value"]; ok {
				return renderJSONValue(inner, depth+1)
			}
		}
		if depth > 0 {
			b, _ := json.Marshal(t)
			return string(b)
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			if s := renderJSONValue(t[k], depth+1); s != "" {
				parts = append(parts, k+": "+s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

// RawJSON passes a stored JSONB document through unchanged (compacted); an
// unreadable document serializes as null rather than breaking the response.
func RawJSON(b []byte) json.RawMessage {
	if len(b) == 0 || !json.Valid(b) {
		return json.RawMessage("null")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(buf.Bytes())
}

// JSONArray parses a log_events *_json column that should hold a JSON
// array; ok is false when the string is empty or not a JSON array.
func JSONArray(s string) (json.RawMessage, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !json.Valid([]byte(s)) {
		return nil, false
	}
	var items []json.RawMessage
	if json.Unmarshal([]byte(s), &items) != nil {
		return nil, false
	}
	return RawJSON([]byte(s)), true
}

// RiskComponents is the parsed log_events.risk_components_json ([] when the
// column holds no array); null without the event row.
func RiskComponents(ev *AlertEventRow) json.RawMessage {
	if ev == nil {
		return json.RawMessage("null")
	}
	if arr, ok := JSONArray(ev.RiskComponentsJSON); ok {
		return arr
	}
	return json.RawMessage("[]")
}

// RiskReasons is the parsed log_events.risk_reasons_json; when the column
// holds no array the pipeline's alert reason is the only reason.
func RiskReasons(ev *AlertEventRow, alertReason string) json.RawMessage {
	if ev != nil {
		if arr, ok := JSONArray(ev.RiskReasonsJSON); ok {
			return arr
		}
	}
	b, _ := json.Marshal([]string{alertReason})
	return json.RawMessage(b)
}

// EventRiskScoreOf is log_events.event_risk_score (Nullable) as an int pointer.
func EventRiskScoreOf(ev *AlertEventRow) *int {
	if ev == nil || ev.EventRiskScore == nil {
		return nil
	}
	return IntPtr(int(*ev.EventRiskScore))
}

// AlertEventIDs lists the distinct event ids of a page, in page order.
func AlertEventIDs(rows []AlertRow) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(rows))
	for _, a := range rows {
		if _, dup := seen[a.EventID]; dup || a.EventID == "" {
			continue
		}
		seen[a.EventID] = struct{}{}
		out = append(out, a.EventID)
	}
	return out
}

// AlertEvents reads the log_events rows of a page in one batch; a disabled
// or failing event store yields an empty map (logged), so every
// event-backed field becomes null instead of failing the request.
func AlertEvents(ctx context.Context, events *MonitoringEvents, op string, rows []AlertRow) map[string]AlertEventRow {
	ids := AlertEventIDs(rows)
	if len(ids) == 0 {
		return map[string]AlertEventRow{}
	}
	found, err := events.EventsByID(ctx, ids)
	if !EventsReadOK(op, err) {
		return map[string]AlertEventRow{}
	}
	return found
}

// EventOf returns the event row of an alert, or nil.
func EventOf(events map[string]AlertEventRow, a AlertRow) *AlertEventRow {
	if ev, ok := events[a.EventID]; ok {
		return &ev
	}
	return nil
}
