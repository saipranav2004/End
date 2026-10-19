package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/scope"
)

// PermissionSourceRepository answers "which policy granted this event" from a
// credential's effective_permissions document.
type PermissionSourceRepository struct{}

func NewPermissionSourceRepository() *PermissionSourceRepository {
	return &PermissionSourceRepository{}
}

const policyMayHaveChangedNote = "Policy may have changed since this event"

// Find looks up the grant that covers an event (service short name such as
// "s3", and the CloudTrail event name such as "GetObject") for one credential.
// The credential must be inside the caller's accounts and regions;
// otherwise ErrCredentialNotFound is returned. A credential with no matching
// grant is a normal answer (Matched=false), not an error.
func (r *PermissionSourceRepository) Find(ctx context.Context, resolved *scope.DataScope, credentialID, service, eventName string) (*models.PermissionSourceResponse, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, ErrCredentialNotFound
	}
	query := fmt.Sprintf(`
		SELECT ep.permissions
		FROM %[1]s.credentials c
		LEFT JOIN %[1]s.effective_permissions ep ON ep.credential_id::text = c.id::text
		WHERE c.id = $1::uuid
		  AND c.account_id = ANY($2::text[]) AND c.region = ANY($3::text[])
		  AND c.deleted_at IS NULL
		LIMIT 1`, resolved.QSchema())

	var raw []byte
	err := resolved.DB.QueryRowContext(ctx, query, credentialID,
		pq.Array(resolved.AccountIDs), pq.Array(resolved.Regions)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load effective permissions: %w", err)
	}

	var entries []map[string]interface{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &entries)
	}
	return grantResponse(bestGrant(entries, service, eventName)), nil
}

// grant is one effective-permission entry that covers an event.
type grant struct {
	policySource, policyType, effect, resourceARN, action string
}

// bestGrant picks the entry that explains an event. An exact action beats a
// wildcard one, and an Allow beats a Deny (so the grant that let the call
// through is the one reported); ties keep the first entry. nil means no entry
// covers the event.
func bestGrant(entries []map[string]interface{}, service, eventName string) *grant {
	str := func(m map[string]interface{}, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	var best *grant
	bestScore := -1
	for _, e := range entries {
		svc, action := str(e, "service", "Service"), str(e, "action", "Action")
		if !matchGranted(svc, action, service, eventName) {
			continue
		}
		effect := str(e, "effect", "Effect")
		if effect == "" {
			effect = "Allow"
		}
		_, bare := splitGrantedAction(svc, action)
		score := 0
		if !strings.ContainsAny(bare, "*?") {
			score += 2 // exact action
		}
		if strings.EqualFold(effect, "Allow") {
			score += 4
		}
		if score <= bestScore {
			continue
		}
		bestScore = score
		resource := str(e, "resource_arn", "resource", "Resource")
		if resource == "" {
			resource = "*"
		}
		best = &grant{
			policySource: str(e, "policy_source", "policy_name", "PolicySource"),
			policyType:   str(e, "policy_type", "PolicyType"),
			effect:       effect,
			resourceARN:  resource,
			action:       action,
		}
	}
	return best
}

func grantResponse(g *grant) *models.PermissionSourceResponse {
	if g == nil {
		note := policyMayHaveChangedNote
		return &models.PermissionSourceResponse{Matched: false, Note: &note}
	}
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	return &models.PermissionSourceResponse{
		Matched:       true,
		PolicySource:  opt(g.policySource),
		PolicyType:    opt(g.policyType),
		Effect:        opt(g.effect),
		ResourceARN:   opt(g.resourceARN),
		MatchedAction: opt(g.action),
	}
}
