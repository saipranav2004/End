package genome

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

// Shared by every <api>_repo.go of this package: the normalized monitoring
// key and the helpers that bind a list of keys into one PostgreSQL query
// (unnest).

// ErrMonitoringTableMissing is returned when a pipeline table does not exist
// in the tenant database (the monitoring migrations have not run there).
// The service turns it into null cards instead of an error.
var ErrMonitoringTableMissing = errors.New("monitoring table is not available")

// IsMonitoringTableMissing reports whether err wraps ErrMonitoringTableMissing.
func IsMonitoringTableMissing(err error) bool { return errors.Is(err, ErrMonitoringTableMissing) }

func wrapMonitoringErr(op string, err error) error {
	if err == nil {
		return nil
	}
	// The pool runs on pgx; lib/pq errors are still recognised.
	var pgErr *pgconn.PgError
	var pqErr *pq.Error
	if (errors.As(err, &pgErr) && pgErr.Code == "42P01") || (errors.As(err, &pqErr) && pqErr.Code == "42P01") { // undefined_table
		return fmt.Errorf("%s: %w: %v", op, ErrMonitoringTableMissing, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// MonitoringKey is the normalized monitoring identity key.
type MonitoringKey struct {
	SourceType   string
	IdentityName string
}

// keyArrays splits keys into the two parallel arrays bound as $1/$2 and
// joined with unnest(...) so one query serves any number of keys.
func keyArrays(keys []MonitoringKey) (pq.StringArray, pq.StringArray) {
	types := make(pq.StringArray, 0, len(keys))
	names := make(pq.StringArray, 0, len(keys))
	for _, k := range keys {
		types = append(types, k.SourceType)
		names = append(names, k.IdentityName)
	}
	return types, names
}

const keyJoinSQL = ` JOIN unnest($1::text[], $2::text[]) AS k(source_type, identity_name)
        ON k.source_type = %[1]s.source_type AND k.identity_name = %[1]s.identity_name `

func keyJoin(alias string) string { return fmt.Sprintf(keyJoinSQL, alias) }

// keyPreferenceJoinSQL joins the key list like keyJoinSQL but keeps the
// caller's order as k.preference, so "first key that has a row" is a
// deterministic ORDER BY k.preference LIMIT 1.
const keyPreferenceJoinSQL = ` JOIN unnest($1::text[], $2::text[]) WITH ORDINALITY AS k(source_type, identity_name, preference)
        ON k.source_type = %[1]s.source_type AND k.identity_name = %[1]s.identity_name `
