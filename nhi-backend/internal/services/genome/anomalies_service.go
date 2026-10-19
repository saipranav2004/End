package genome

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	models "nhi-backend-api/internal/models/genome"
	repo "nhi-backend-api/internal/repository/genome"
)

// AnomaliesService implements GET /genome/anomalies and
// POST /genome/anomalies/{alert_id}/feedback (backend-nhi-genome.md §6.1,
// §6.4). Anomalies are listed only for identities in the caller's scope —
// the same discovery fleet the identity endpoints use — so an alert of an
// identity outside the scope is neither listed nor writable.
type AnomaliesService struct {
	lookup *MonitoredIdentityLookup
	repo   *repo.AnomaliesRepository
	events *repo.MonitoringEvents // nil when no monitoring ClickHouse is configured
}

// NewAnomaliesService wires the service.
func NewAnomaliesService(lookup *MonitoredIdentityLookup, repo *repo.AnomaliesRepository, events *repo.MonitoringEvents) *AnomaliesService {
	return &AnomaliesService{lookup: lookup, repo: repo, events: events}
}

// Errors the handler maps to responses.
var (
	ErrAnomalyNotFound      = errors.New("anomaly not found")
	ErrFeedbackUnavailable  = errors.New("anomaly feedback is not available")
	ErrAnomalyNotReviewable = errors.New("anomaly has no event to attach feedback to")
	ErrFeedbackForbidden    = errors.New("only analysts and admins can review anomalies")
)

// FeedbackValidationError is a request the handler answers with 400.
type FeedbackValidationError struct{ Msg string }

func (e *FeedbackValidationError) Error() string { return e.Msg }

// Review filter values of the list.
const (
	ReviewPending  = "pending"
	ReviewReviewed = "reviewed"
)

// AnomalyListParams are the parsed list query parameters (§6.1 plus review
// and identity_name). Empty strings mean "no filter".
type AnomalyListParams struct {
	SourceType   string
	IdentityName string // exact identity key; with SourceType, one identity's anomalies
	Search       string
	Dimension    string
	State        string
	Review       string
	From, To     *time.Time
	Limit        int
	Cursor       string
}

// List builds one page of anomalies, newest first, with the review counts
// of every anomaly matching the filters (review aside).
func (s *AnomaliesService) List(ctx context.Context, userID, tenantID, dbName string, p AnomalyListParams) (*models.AnomalyListResponse, *string, error) {
	after, err := decodeAnomalyCursor(p.Cursor)
	if err != nil {
		return nil, nil, err
	}
	sc, fleet, err := s.lookup.Fleet(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, nil, err
	}
	keys, owners := anomalyKeys(fleet, p)

	withFeedback, err := s.repo.FeedbackTableExists(ctx, sc)
	if err != nil {
		return nil, nil, &StoreUnavailableError{Op: "read anomalies", Err: err}
	}
	filter := repo.AnomalyFilter{Dimension: p.Dimension, State: p.State, From: p.From, To: p.To, Review: p.Review}
	out := &models.AnomalyListResponse{Items: []models.AnomalyItem{}, FeedbackAvailable: withFeedback}

	counts, err := s.repo.Counts(ctx, sc, keys, filter, withFeedback)
	if repo.IsMonitoringTableMissing(err) {
		return out, nil, nil // no alerts table yet: nothing to list
	}
	if err != nil {
		return nil, nil, &StoreUnavailableError{Op: "read anomalies", Err: err}
	}
	out.Counts = models.AnomalyCounts{
		Total: counts.Total, NeedsReview: counts.Pending, Reviewed: counts.Total - counts.Pending,
		RealAnomaly: counts.Malicious, FalsePositive: counts.Legitimate,
	}
	if !withFeedback && p.Review == ReviewReviewed {
		return out, nil, nil // nothing can have been reviewed
	}

	rows, err := s.repo.Page(ctx, sc, keys, filter, withFeedback, after, p.Limit)
	if err != nil {
		return nil, nil, &StoreUnavailableError{Op: "read anomalies", Err: err}
	}
	var next *string
	if len(rows) > p.Limit {
		rows = rows[:p.Limit]
		last := rows[len(rows)-1]
		next = encodeAnomalyCursor(repo.AnomalyCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}

	eventIDs := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.EventID != "" {
			eventIDs = append(eventIDs, r.EventID)
		}
	}
	events, err := s.events.EventsByID(ctx, eventIDs)
	eventsOK := eventsReadOK("anomaly events", err)

	for _, r := range rows {
		item := anomalyItem(r, owners[r.Key], withFeedback)
		if eventsOK {
			if ev, ok := events[r.EventID]; ok {
				t := ev.EventTime
				item.Event = &models.AnomalyEvent{EventTime: &t, EventName: ev.EventName, EventSource: ev.EventSource,
					Region: ev.Region, SourceIP: ev.SourceIP, RiskScore: ev.RiskScore}
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, next, nil
}

// anomalyKeys returns the monitoring keys whose alerts may be listed (each
// identity's key and its shared workload key) and the identity each key
// belongs to. The search and identity filters narrow the fleet first.
func anomalyKeys(fleet []monitoredIdentity, p AnomalyListParams) ([]repo.MonitoringKey, map[repo.MonitoringKey]monitoredIdentity) {
	needle := strings.ToLower(strings.TrimSpace(p.Search))
	owners := map[repo.MonitoringKey]monitoredIdentity{}
	keys := []repo.MonitoringKey{}
	for _, id := range fleet {
		if p.SourceType != "" && id.Key.SourceType != p.SourceType {
			continue
		}
		if p.IdentityName != "" && id.Key.IdentityName != p.IdentityName {
			continue
		}
		if needle != "" && !identityMatches(id, needle) {
			continue
		}
		for _, k := range id.allKeys() {
			if _, taken := owners[k]; taken {
				continue // a shared workload key belongs to the first identity listing it
			}
			owners[k] = id
			keys = append(keys, k)
		}
	}
	return keys, owners
}

func identityMatches(id monitoredIdentity, needle string) bool {
	for _, v := range []string{id.Key.IdentityName, id.DisplayName, id.WorkloadKey, id.ServiceName, id.RoleARN} {
		if v != "" && strings.Contains(strings.ToLower(v), needle) {
			return true
		}
	}
	return false
}

func anomalyItem(r repo.AlertRow, owner monitoredIdentity, withFeedback bool) models.AnomalyItem {
	ref := owner.ref()
	if ref.IdentityName == "" { // not expected: every listed key has an owner
		ref = models.DashboardIdentityRef{ID: r.Key.SourceType + ":" + r.Key.IdentityName, SourceType: r.Key.SourceType, IdentityName: r.Key.IdentityName, DisplayName: r.Key.IdentityName}
	}
	item := models.AnomalyItem{
		ID: r.ID, Identity: ref, AccountID: owner.AccountID,
		WhatChanged: r.Reason, Dimension: r.Dimension, Observed: r.Observed, Baseline: r.Baseline,
		State: r.Status, DetectedAt: r.CreatedAt,
		CanReview: withFeedback && r.EventID != "",
	}
	if r.Feedback != nil {
		item.Feedback = feedbackModel(r.Feedback)
	}
	return item
}

func feedbackModel(f *repo.FeedbackRow) *models.AnomalyFeedback {
	return &models.AnomalyFeedback{Decision: f.Decision, ApprovedForBaseline: f.ApprovedForBaseline,
		Reviewer: f.Reviewer, Reason: f.Reason, UpdatedAt: f.UpdatedAt}
}

// FeedbackCaller is who is reviewing.
type FeedbackCaller struct {
	UserID, TenantID, DBName, Role, Reviewer string
}

// maxFeedbackReason bounds the free-text reason.
const maxFeedbackReason = 1000

// SaveFeedback records a reviewer decision on one anomaly (§6.4). It does
// not change the alert's state or the baseline.
func (s *AnomaliesService) SaveFeedback(ctx context.Context, c FeedbackCaller, alertID int64, req models.AnomalyFeedbackRequest) (*models.AnomalyFeedback, error) {
	switch strings.ToLower(strings.TrimSpace(c.Role)) {
	case "analyst", "admin":
	default:
		return nil, ErrFeedbackForbidden
	}
	decision := strings.TrimSpace(req.Decision)
	switch decision {
	case models.FeedbackLegitimate, models.FeedbackMalicious, models.FeedbackUnknown:
	default:
		return nil, &FeedbackValidationError{Msg: "decision must be legitimate, malicious or unknown"}
	}
	approved := req.ApprovedForBaseline != nil && *req.ApprovedForBaseline
	if approved && decision != models.FeedbackLegitimate {
		return nil, &FeedbackValidationError{Msg: "approved_for_baseline can be true only when the decision is legitimate"}
	}
	var reason *string
	if req.Reason != nil {
		if v := strings.TrimSpace(*req.Reason); v != "" {
			if utf8.RuneCountInString(v) > maxFeedbackReason {
				return nil, &FeedbackValidationError{Msg: "reason must be at most 1000 characters"}
			}
			reason = &v
		}
	}

	sc, fleet, err := s.lookup.Fleet(ctx, c.UserID, c.TenantID, c.DBName)
	if err != nil {
		return nil, err
	}
	withFeedback, err := s.repo.FeedbackTableExists(ctx, sc)
	if err != nil {
		return nil, &StoreUnavailableError{Op: "read anomaly", Err: err}
	}
	alert, err := s.repo.Alert(ctx, sc, alertID, withFeedback)
	if errors.Is(err, repo.ErrAlertNotFound) || repo.IsMonitoringTableMissing(err) {
		return nil, ErrAnomalyNotFound
	}
	if err != nil {
		return nil, &StoreUnavailableError{Op: "read anomaly", Err: err}
	}
	if _, owners := anomalyKeys(fleet, AnomalyListParams{}); !hasOwner(owners, alert.Key) {
		return nil, ErrAnomalyNotFound // outside the caller's scope
	}
	if !withFeedback {
		return nil, ErrFeedbackUnavailable
	}
	if alert.EventID == "" {
		return nil, ErrAnomalyNotReviewable
	}
	fb, err := s.repo.SaveFeedback(ctx, sc, repo.FeedbackWrite{
		Key: alert.Key, EventID: alert.EventID, Decision: decision,
		ApprovedForBaseline: approved, Reviewer: c.Reviewer, Reason: reason,
	})
	if err != nil {
		return nil, &StoreUnavailableError{Op: "save anomaly feedback", Err: err}
	}
	return feedbackModel(fb), nil
}

func hasOwner(owners map[repo.MonitoringKey]monitoredIdentity, k repo.MonitoringKey) bool {
	_, ok := owners[k]
	return ok
}

// ---------------------------------------------------------------------------
// keyset cursor
// ---------------------------------------------------------------------------

type anomalyCursorJSON struct {
	T  int64 `json:"t"` // created_at, unix microseconds
	ID int64 `json:"i"`
}

func encodeAnomalyCursor(c repo.AnomalyCursor) *string {
	raw, _ := json.Marshal(anomalyCursorJSON{T: c.CreatedAt.UnixMicro(), ID: c.ID})
	s := "a." + base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

func decodeAnomalyCursor(s string) (*repo.AnomalyCursor, error) {
	if s == "" {
		return nil, nil
	}
	if !strings.HasPrefix(s, "a.") {
		return nil, ErrInvalidCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(s[2:])
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var c anomalyCursorJSON
	if err := json.Unmarshal(raw, &c); err != nil || c.ID <= 0 || c.T <= 0 {
		return nil, ErrInvalidCursor
	}
	return &repo.AnomalyCursor{CreatedAt: time.UnixMicro(c.T).UTC(), ID: c.ID}, nil
}
