package genome

import (
	"errors"
	"fmt"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

// Shared by every <api>_repo.go of this package: the normalized monitoring
// key and the helpers that bind a list of keys into one PostgreSQL query
// (unnest) or one ClickHouse query (has()).

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

// keyFilterSQL restricts log_events rows to the requested identity keys. The
// two has() tests form a superset (every requested source_type combined
// with every requested identity_name); callers keep only exact pairs.
const keyFilterSQL = ` has({types:Array(String)}, source_type)
	  AND has({names:Array(String)}, identity_name) `

func keyFilterArgs(keys []MonitoringKey) []any {
	types := make([]string, 0, len(keys))
	names := make([]string, 0, len(keys))
	seenType := map[string]struct{}{}
	for _, k := range keys {
		if _, ok := seenType[k.SourceType]; !ok {
			seenType[k.SourceType] = struct{}{}
			types = append(types, k.SourceType)
		}
		names = append(names, k.IdentityName)
	}
	return []any{ch.Named("types", types), ch.Named("names", names)}
}

// keySet indexes the requested keys for the exact-pair filter.
func keySet(keys []MonitoringKey) map[MonitoringKey]struct{} {
	set := make(map[MonitoringKey]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return set
}
