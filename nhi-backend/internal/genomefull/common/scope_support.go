package common

import (
	"context"
	"errors"
	"log"
	"time"

	"nhi-backend-api/internal/database"
	"nhi-backend-api/internal/scope"
)

// ScopeResolver is the part of scope.Resolver every genome service uses.
type ScopeResolver interface {
	Resolve(ctx context.Context, userID, tenantID, dbName string) (*scope.DataScope, error)
}

// TimeWindow is a half-open UTC range [From, To).
type TimeWindow struct {
	From time.Time
	To   time.Time
}

// ErrInvalidCursor reports a cursor that was not produced by the endpoint
// that received it; handlers answer 400 VALIDATION_ERROR.
var ErrInvalidCursor = errors.New("cursor is invalid")

// StoreUnavailableError means the tenant scope or a monitoring table could
// not be read, so there is no data at all; the handler maps it to 503.
type StoreUnavailableError struct {
	Op  string
	Err error
}

func (e *StoreUnavailableError) Error() string { return e.Op + ": " + e.Err.Error() }
func (e *StoreUnavailableError) Unwrap() error { return e.Err }

// Reason is the user-safe message for the 503 envelope: it names the
// operation and the classified cause, never the driver error, SQL or host.
func (e *StoreUnavailableError) Reason() string {
	switch {
	case errors.Is(e.Err, database.ErrTenantDBNotConfigured):
		return "the tenant database is not configured for this account"
	case errors.Is(e.Err, ErrMonitoringTableMissing):
		return e.Op + ": the table is not available in the tenant schema"
	case errors.Is(e.Err, ErrStoreSchemaMismatch):
		return e.Op + ": the table schema does not match what the API expects"
	case errors.Is(e.Err, ErrStorePermission):
		return e.Op + ": the API database role lacks permission"
	case errors.Is(e.Err, ErrStoreUnreachable):
		return e.Op + ": the database is unreachable"
	default:
		return e.Op + " failed"
	}
}

// IsStoreTimeout reports whether err is the request's time budget running
// out — in a PostgreSQL call, a ClickHouse call or between them.
func IsStoreTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrStoreTimeout) ||
		errors.Is(err, ErrEventsTimeout)
}

// InvalidInputError is a request that passed the handler's validation but
// was rejected by the store on input grounds (a CHECK/UNIQUE/NOT NULL
// constraint or a value that does not fit the column). Handlers answer 400
// VALIDATION_ERROR with Msg; nothing about the store leaks.
type InvalidInputError struct{ Msg string }

func (e *InvalidInputError) Error() string { return e.Msg }

// BudgetExceeded converts an exhausted request budget (or a client that
// disconnected) into a *StoreUnavailableError once the optional reads are
// done, so a response assembled after the deadline is never sent as a
// complete 200 with silently-null fields.
func BudgetExceeded(ctx context.Context, op string) error {
	if err := ctx.Err(); err != nil {
		return &StoreUnavailableError{Op: op, Err: err}
	}
	return nil
}

// ReadOK reports whether a monitoring-table (PostgreSQL) read succeeded;
// failures are logged with their classified cause and leave the dependent
// fields null/empty (graceful degradation). Context failures (request
// budget exhausted, client gone) are not logged here.
func ReadOK(op string, err error) bool {
	if err == nil {
		return true
	}
	switch {
	case IsContextError(err):
	case errors.Is(err, ErrMonitoringTableMissing):
		log.Printf("genome: %s: table not present in the tenant schema (pipeline migrations not run?): %v", op, err)
	case errors.Is(err, ErrStoreSchemaMismatch):
		log.Printf("genome: %s: SCHEMA MISMATCH between the pipeline table and the API query: %v", op, err)
	case errors.Is(err, ErrStorePermission):
		log.Printf("genome: %s: PERMISSION DENIED for the API database role (GRANT missing?): %v", op, err)
	case errors.Is(err, ErrStoreUnreachable):
		log.Printf("genome: %s: database unreachable: %v", op, err)
	default:
		log.Printf("genome: %s failed: %v", op, err)
	}
	return false
}

// EventsReadOK reports whether a ClickHouse read succeeded; failures are
// logged with their classified cause and leave the dependent fields null,
// like ReadOK for the PostgreSQL tables. A not-configured store and context
// failures are not logged here.
func EventsReadOK(op string, err error) bool {
	if err == nil {
		return true
	}
	switch {
	case errors.Is(err, ErrMonitoringEventsNotConfigured), IsContextError(err):
	case errors.Is(err, ErrEventsTableMissing):
		log.Printf("genome: %s: clickhouse database or configured Genome table not found (check NHI_CLICKHOUSE_DATABASE / NHI_CLICKHOUSE_GENOME_TABLE): %v", op, err)
	case errors.Is(err, ErrEventsSchemaMismatch):
		log.Printf("genome: %s: SCHEMA MISMATCH between the configured Genome table and the API query: %v", op, err)
	case errors.Is(err, ErrEventsAccessDenied):
		log.Printf("genome: %s: clickhouse ACCESS DENIED (check NHI_CLICKHOUSE_USER / _PASSWORD and grants on the configured Genome table): %v", op, err)
	case errors.Is(err, ErrEventsTimeout):
		log.Printf("genome: %s: clickhouse query timed out (NHI_CLICKHOUSE_TIMEOUT_SECONDS): %v", op, err)
	default:
		log.Printf("genome: %s: configured Genome ClickHouse table read failed: %v", op, err)
	}
	return false
}
