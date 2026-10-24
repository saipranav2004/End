package anomalyfeedback

import (
	"context"
	"errors"
	"strings"

	"nhi-backend-api/internal/genomefull/common"
)

// Service implements POST /api/v1/genome/anomalies/{alert_id}/feedback,
// the only write of the genome API: it stores reviewer feedback for the
// alert's event as one alert_feedback row keyed by (account_id, source_type,
// event_id) — inserted the first time, replaced on later reviews. The
// reviewer is the authenticated user (access token), the alerts row is
// never modified and nothing is written to the event store (feedback is
// not workflow state).
type Service struct {
	resolver common.ScopeResolver
	repo     *Repository
}

// NewService wires the service.
func NewService(repo *Repository, resolver common.ScopeResolver) *Service {
	return &Service{resolver: resolver, repo: repo}
}

// Input is the validated body plus the reviewer taken from the token.
type Input struct {
	Decision            string // legitimate | malicious | unknown (validated by the handler)
	ApprovedForBaseline bool
	Reason              *string
	Reviewer            string
}

// Save upserts the feedback row. Unknown ids are common.ErrAnomalyNotFound;
// a tenant whose alert_feedback table is missing or unreachable gets
// *common.StoreUnavailableError (the write cannot be performed); a row the
// table's own constraints reject is *common.InvalidInputError (400).
func (s *Service) Save(ctx context.Context, userID, tenantID, dbName string, alertID int64, in Input) (*Response, error) {
	sc, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, &common.StoreUnavailableError{Op: "resolve tenant scope", Err: err}
	}
	a, err := s.repo.Alert(ctx, sc, alertID)
	if err != nil {
		return nil, &common.StoreUnavailableError{Op: "read alert", Err: err}
	}
	if a == nil {
		return nil, common.ErrAnomalyNotFound
	}
	accountID := common.AnomalyAccountID(*a, nil)
	if accountID == nil { // an alert without account metadata: the identity's ARN carries the account
		if parts := strings.Split(a.Key.IdentityName, ":"); len(parts) > 5 && parts[0] == "arn" && len(parts[4]) == 12 {
			acct := parts[4]
			accountID = &acct
		}
	}
	if accountID == nil {
		return nil, &common.StoreUnavailableError{
			Op:  "resolve alert feedback account_id",
			Err: errors.New("alerts.identity_metadata.account_id is missing"),
		}
	}
	row, err := s.repo.Upsert(ctx, sc, Write{
		AccountID:           *accountID,
		SourceType:          a.Key.SourceType,
		EventID:             a.EventID,
		Decision:            in.Decision,
		ApprovedForBaseline: in.ApprovedForBaseline,
		Reviewer:            common.OptionalString(in.Reviewer),
		Reason:              in.Reason,
	})
	if err != nil {
		var cv *common.ConstraintViolation
		if errors.As(err, &cv) {
			return nil, &common.InvalidInputError{Msg: "feedback was rejected by the alert_feedback table: " + cv.Describe()}
		}
		return nil, &common.StoreUnavailableError{Op: "write alert feedback", Err: err}
	}
	return &Response{
		AlertID: a.ID,
		Feedback: common.Feedback{
			Decision:            row.Decision,
			Reviewer:            row.Reviewer,
			Reason:              row.Reason,
			ApprovedForBaseline: row.ApprovedForBaseline,
		},
		UpdatedAt: row.UpdatedAt,
	}, nil
}
