package common

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/lib/pq"
)

// Shared by every endpoint's repo.go: the error classification every
// PostgreSQL read/write goes through, the normalized monitoring key and the
// helpers that bind a list of keys into one PostgreSQL query (unnest) or one
// ClickHouse query (has()).

// ---------------------------------------------------------------------------
// Error classification
// ---------------------------------------------------------------------------
//
// Every repository call returns errors through WrapMonitoringErr so the
// service and handler layers can decide with errors.Is / errors.As instead
// of string matching. The original driver error is always kept in the chain
// (%w) for the log line.

var (
	// ErrMonitoringTableMissing: the table (or the tenant schema itself)
	// does not exist in the tenant database — the pipeline migrations have
	// not run there. Reads degrade to null/empty.
	ErrMonitoringTableMissing = errors.New("monitoring table is not available in the tenant schema")
	// ErrStoreSchemaMismatch: the table exists but a column or type the API
	// relies on does not (pipeline schema drift). Reads degrade like a
	// missing table but the log line says what to fix.
	ErrStoreSchemaMismatch = errors.New("monitoring table schema does not match the API")
	// ErrStorePermission: the API database role lacks a privilege on the
	// table (GRANT missing).
	ErrStorePermission = errors.New("database permission denied")
	// ErrStoreTimeout: the statement was cancelled because the request's
	// time budget ran out (handler → 504).
	ErrStoreTimeout = errors.New("database query timed out")
	// ErrStoreUnreachable: connection-level failures (connection refused or
	// dropped, server shutting down, pool exhausted) — retryable (503).
	ErrStoreUnreachable = errors.New("database is unreachable")
)

// ConstraintViolation is a PostgreSQL data/integrity error (SQLSTATE class
// 22 or 23) raised while writing: the input violated a CHECK/UNIQUE/foreign
// key/NOT NULL constraint or did not fit the column. It is the caller's
// input that is wrong, so the handler answers 400, not 503.
type ConstraintViolation struct {
	Code       string // SQLSTATE
	Constraint string // constraint name when PostgreSQL reports one
	Column     string // column name when PostgreSQL reports one
	Message    string // PostgreSQL's primary message (no user data)
}

func (e *ConstraintViolation) Error() string {
	return "constraint violation " + e.Code + ": " + e.Describe()
}

// Describe returns a short, user-safe description of the violation: the
// kind of constraint and its name/column, falling back to PostgreSQL's
// primary message only when it carries no object name of its own.
func (e *ConstraintViolation) Describe() string {
	switch e.Code {
	case "23502": // not_null_violation
		if e.Column != "" {
			return "column " + e.Column + " must not be null"
		}
		return "a required column is null"
	case "23503": // foreign_key_violation
		return "violates foreign key constraint " + e.Constraint
	case "23505": // unique_violation
		return "violates unique constraint " + e.Constraint
	case "23514": // check_violation
		return "violates check constraint " + e.Constraint
	case "22001": // string_data_right_truncation
		return "a value is too long for its column"
	case "22P02", "22007", "22008": // invalid_text_representation, invalid_datetime_format, datetime_field_overflow
		return "a value has the wrong format for its column"
	}
	if e.Constraint != "" {
		return "violates constraint " + e.Constraint
	}
	if e.Column != "" {
		return "invalid value for column " + e.Column
	}
	return e.Message
}

// WrapMonitoringErr classifies a PostgreSQL error and prefixes it with the
// operation name. ctx is the context the statement ran under: a cancelled
// statement (SQLSTATE 57014) is only a timeout when our deadline expired.
func WrapMonitoringErr(ctx context.Context, op string, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		// Whatever the driver reported, the request context ended first.
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return fmt.Errorf("%s: %w: %w", op, ErrStoreTimeout, ctxErr)
		}
		return fmt.Errorf("%s: %w", op, ctxErr) // client went away
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", op, ErrStoreTimeout, err)
	}
	if errors.Is(err, driver.ErrBadConn) {
		return fmt.Errorf("%s: %w: %w", op, ErrStoreUnreachable, err)
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		code := string(pqErr.Code)
		switch {
		case code == "42P01", code == "3F000": // undefined_table, invalid_schema_name
			return fmt.Errorf("%s: %w: %w", op, ErrMonitoringTableMissing, err)
		case code == "42703", code == "42804", code == "42883": // undefined_column, datatype_mismatch, undefined_function
			return fmt.Errorf("%s: %w: %w", op, ErrStoreSchemaMismatch, err)
		case code == "42501": // insufficient_privilege
			return fmt.Errorf("%s: %w: %w", op, ErrStorePermission, err)
		case code == "57014": // query_canceled (statement_timeout or our cancel)
			return fmt.Errorf("%s: %w: %w", op, ErrStoreTimeout, err)
		case strings.HasPrefix(code, "08"), // connection_exception
			strings.HasPrefix(code, "53"),  // insufficient_resources (too_many_connections, …)
			strings.HasPrefix(code, "57P"): // admin_shutdown, crash_shutdown, cannot_connect_now
			return fmt.Errorf("%s: %w: %w", op, ErrStoreUnreachable, err)
		case strings.HasPrefix(code, "22"), strings.HasPrefix(code, "23"): // data_exception, integrity_constraint_violation
			return fmt.Errorf("%s: %w", op, &ConstraintViolation{
				Code:       code,
				Constraint: pqErr.Constraint,
				Column:     pqErr.Column,
				Message:    pqErr.Message,
			})
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}

// IsMonitoringTableMissing reports whether err wraps ErrMonitoringTableMissing.
func IsMonitoringTableMissing(err error) bool { return errors.Is(err, ErrMonitoringTableMissing) }

// IsStoreDegradable reports whether a *read* failure should degrade to
// null/empty fields (the data simply is not there to read) rather than fail
// the request: the table is missing, its schema drifted, or the role cannot
// read it. Connection failures and timeouts are not degradable.
func IsStoreDegradable(err error) bool {
	return errors.Is(err, ErrMonitoringTableMissing) ||
		errors.Is(err, ErrStoreSchemaMismatch) ||
		errors.Is(err, ErrStorePermission)
}

// IsContextError reports whether err stems from the request context ending
// (deadline or client disconnect) rather than from the store.
func IsContextError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, ErrStoreTimeout)
}

// MonitoringKey is the normalized monitoring identity key.
type MonitoringKey struct {
	SourceType   string
	IdentityName string
}

// KeyArrays splits keys into the two parallel arrays bound as $1/$2 and
// joined with unnest(...) so one query serves any number of keys.
func KeyArrays(keys []MonitoringKey) (pq.StringArray, pq.StringArray) {
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

// KeyJoin returns the SQL fragment joining a table aliased alias to the
// $1/$2 key arrays.
func KeyJoin(alias string) string { return fmt.Sprintf(keyJoinSQL, alias) }

// keyPreferenceJoinSQL joins the key list like KeyJoin but keeps the
// caller's order as k.preference, so "first key that has a row" is a
// deterministic ORDER BY k.preference LIMIT 1.
const keyPreferenceJoinSQL = ` JOIN unnest($1::text[], $2::text[]) WITH ORDINALITY AS k(source_type, identity_name, preference)
        ON k.source_type = %[1]s.source_type AND k.identity_name = %[1]s.identity_name `

// KeyPreferenceJoin returns the SQL fragment of keyPreferenceJoinSQL for alias.
func KeyPreferenceJoin(alias string) string { return fmt.Sprintf(keyPreferenceJoinSQL, alias) }

// KeyFilterSQL restricts configured Genome event-table rows to the requested identity keys. The
// two has() tests form a superset (every requested source_type combined
// with every requested identity_name); callers keep only exact pairs.
const KeyFilterSQL = ` has({types:Array(String)}, source_type)
	  AND has({names:Array(String)}, identity_name) `

// KeyFilterArgs are the ClickHouse named parameters of KeyFilterSQL.
func KeyFilterArgs(keys []MonitoringKey) []any {
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

// KeySet indexes the requested keys for the exact-pair filter.
func KeySet(keys []MonitoringKey) map[MonitoringKey]struct{} {
	set := make(map[MonitoringKey]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return set
}

// RowScanner is satisfied by *sql.Row and *sql.Rows.
type RowScanner interface {
	Scan(dest ...any) error
}

// EscapeLike escapes %, _ and \ so user text is matched literally by LIKE
// (PostgreSQL's default escape character is the backslash).
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
