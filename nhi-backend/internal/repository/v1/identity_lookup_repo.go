package v1

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

// LookupIdentity is the slice of an identity row the resolver needs.
type LookupIdentity struct {
	ID         string
	ARN        string
	Name       string
	Type       string
	AccountID  string
	PrivateIP  string
	Terminated bool // deleted_at is set
}

// LookupCredential is the slice of a credential row the resolver needs.
type LookupCredential struct {
	ID         string
	ARN        string
	Name       string
	Type       string
	Terminated bool
}

// LookupBinding links one identity to one credential.
type LookupBinding struct {
	IdentityID   string
	CredentialID string
}

// IdentityLookup is everything the resolver needs, loaded once per request.
// Soft-deleted rows are included on purpose so historical events can still
// be attributed to terminated identities and credentials.
type IdentityLookup struct {
	Identities  []LookupIdentity
	Credentials []LookupCredential
	Bindings    []LookupBinding
}

type IdentityLookupRepository struct{}

func NewIdentityLookupRepository() *IdentityLookupRepository {
	return &IdentityLookupRepository{}
}

// TenantAccountIDs returns every account registered for the tenant. This is
// the isolation boundary for ClickHouse queries and for the lookup map; it is
// deliberately wider than the accounts a user ticked in Settings.
func (r *IdentityLookupRepository) TenantAccountIDs(ctx context.Context, tenantDB *sql.DB, schema, tenantID string) ([]string, error) {
	q := fmt.Sprintf(`SELECT DISTINCT account_id FROM %s.accounts
		WHERE tenant_id = $1::uuid AND account_id IS NOT NULL AND account_id <> ''
		ORDER BY account_id`, schema)
	rows, err := tenantDB.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query tenant accounts: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tenant account: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Load reads identities, credentials and their bindings for the given
// accounts in three queries.
func (r *IdentityLookupRepository) Load(ctx context.Context, tenantDB *sql.DB, schema string, accountIDs []string) (*IdentityLookup, error) {
	out := &IdentityLookup{}
	if len(accountIDs) == 0 {
		return out, nil
	}
	accounts := pq.Array(accountIDs)

	idQuery := fmt.Sprintf(`
		SELECT id::text, COALESCE(arn,''), COALESCE(name,''), COALESCE(identity_type,''),
		       COALESCE(account_id,''), COALESCE(type_attributes->>'private_ip',''), deleted_at IS NOT NULL
		FROM %s.identities
		WHERE account_id = ANY($1::text[])`, schema)
	rows, err := tenantDB.QueryContext(ctx, idQuery, accounts)
	if err != nil {
		return nil, fmt.Errorf("query lookup identities: %w", err)
	}
	for rows.Next() {
		var i LookupIdentity
		if err := rows.Scan(&i.ID, &i.ARN, &i.Name, &i.Type, &i.AccountID, &i.PrivateIP, &i.Terminated); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan lookup identity: %w", err)
		}
		out.Identities = append(out.Identities, i)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	credQuery := fmt.Sprintf(`
		SELECT id::text, COALESCE(arn,''), COALESCE(name,''), COALESCE(credential_type,''), deleted_at IS NOT NULL
		FROM %s.credentials
		WHERE account_id = ANY($1::text[])`, schema)
	rows, err = tenantDB.QueryContext(ctx, credQuery, accounts)
	if err != nil {
		return nil, fmt.Errorf("query lookup credentials: %w", err)
	}
	for rows.Next() {
		var c LookupCredential
		if err := rows.Scan(&c.ID, &c.ARN, &c.Name, &c.Type, &c.Terminated); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan lookup credential: %w", err)
		}
		out.Credentials = append(out.Credentials, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Bindings are loaded unfiltered; rows pointing at identities or
	// credentials outside the loaded set are ignored by the resolver.
	bindQuery := fmt.Sprintf(`SELECT identity_id::text, credential_id::text FROM %s.identity_credentials`, schema)
	rows, err = tenantDB.QueryContext(ctx, bindQuery)
	if err != nil {
		return nil, fmt.Errorf("query lookup bindings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var b LookupBinding
		if err := rows.Scan(&b.IdentityID, &b.CredentialID); err != nil {
			return nil, fmt.Errorf("scan lookup binding: %w", err)
		}
		out.Bindings = append(out.Bindings, b)
	}
	return out, rows.Err()
}
