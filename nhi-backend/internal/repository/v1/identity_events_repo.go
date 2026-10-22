package v1

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"nhi-backend-api/internal/logging"

	"github.com/lib/pq"

	models "nhi-backend-api/internal/models/v1"
	"nhi-backend-api/internal/scope"
)

// IdentityEventsRepository loads the Postgres side of the Identity Event
// Graph: the identity and its bound credentials (once per request, so events
// are enriched in memory), and the permission source of a single event.
type IdentityEventsRepository struct{}

func NewIdentityEventsRepository() *IdentityEventsRepository { return &IdentityEventsRepository{} }

// EventsCredential is a credential bound to the identity.
type EventsCredential struct {
	ID          string
	ARN         string
	Name        string
	Type        string
	BindingType string
}

// EventsContext is the identity plus every credential bound to it, restricted
// to the caller's selected accounts and regions.
type EventsContext struct {
	ID           string
	ARN          string
	Name         string
	IdentityType string
	AccountID    string
	Credentials  []EventsCredential
}

// LoadEventContext returns the identity and its bound credentials. It returns
// ErrIdentityNotFound when the identity does not exist or is outside the
// caller's account/region scope.
func (r *IdentityEventsRepository) LoadEventContext(ctx context.Context, resolved *scope.DataScope, id string) (*EventsContext, error) {
	if resolved == nil || resolved.DB == nil {
		return nil, ErrIdentityNotFound
	}
	idQuery := fmt.Sprintf(`
		SELECT id::text, COALESCE(arn,''), COALESCE(name,''), COALESCE(identity_type,''), COALESCE(account_id,'')
		FROM %s.identities
		WHERE id = $1::uuid AND account_id = ANY($2::text[]) AND region = ANY($3::text[])
		  AND deleted_at IS NULL
		LIMIT 1`, resolved.QSchema())
	out := &EventsContext{}
	err := resolved.DB.QueryRowContext(ctx, idQuery, id, pq.Array(resolved.AccountIDs), pq.Array(resolved.Regions)).
		Scan(&out.ID, &out.ARN, &out.Name, &out.IdentityType, &out.AccountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIdentityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load identity for events: %w", err)
	}

	credQuery := fmt.Sprintf(`
		SELECT DISTINCT c.id::text, COALESCE(c.arn,''), COALESCE(c.name,''), COALESCE(c.credential_type,''),
		       COALESCE(ic.binding_type,'')
		FROM %[1]s.identity_credentials ic
		JOIN %[1]s.credentials c ON c.id::text = ic.credential_id::text
		WHERE ic.identity_id::text = $1 AND c.deleted_at IS NULL
		  AND c.account_id = ANY($2::text[]) AND c.region = ANY($3::text[])
		ORDER BY 1`, resolved.QSchema())
	rows, err := resolved.DB.QueryContext(ctx, credQuery, out.ID, pq.Array(resolved.AccountIDs), pq.Array(resolved.Regions))
	if err != nil {
		return nil, fmt.Errorf("load credentials for events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c EventsCredential
		if err := rows.Scan(&c.ID, &c.ARN, &c.Name, &c.Type, &c.BindingType); err != nil {
			return nil, fmt.Errorf("scan credential for events: %w", err)
		}
		out.Credentials = append(out.Credentials, c)
	}
	return out, rows.Err()
}

// GrantFor finds the policy statement that covers an event (service short
// name such as "s3" and the CloudTrail event name) in one credential's
// effective permissions. A credential with no covering statement yields
// Matched=false, not an error. Only the detail section calls this.
func (r *IdentityEventsRepository) GrantFor(ctx context.Context, resolved *scope.DataScope, credentialID, service, eventName string) (*models.PermissionSourceResponse, error) {
	if resolved == nil || resolved.DB == nil || credentialID == "" {
		return grantResponse(nil), nil
	}
	query := fmt.Sprintf(`SELECT permissions FROM %s.effective_permissions WHERE credential_id::text = $1 LIMIT 1`, resolved.QSchema())
	var raw []byte
	err := resolved.DB.QueryRowContext(ctx, query, credentialID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return grantResponse(nil), nil
	}
	if err != nil {
		return nil, fmt.Errorf("load effective permissions: %w", err)
	}
	var entries []map[string]interface{}
	if len(raw) > 0 {
		logging.WarnIf(ctx, "decode stored JSON column", json.Unmarshal(raw, &entries))
	}
	return grantResponse(bestGrant(entries, service, eventName)), nil
}
