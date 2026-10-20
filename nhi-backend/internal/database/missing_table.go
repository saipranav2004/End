package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

// NotProvisionedCode and NotProvisionedMessage are what every API answers
// (503) when the tenant's schema lacks a table it reads: the workspace has
// not been fully set up yet. Not an error the user caused or can fix.
const (
	NotProvisionedCode    = "TENANT_NOT_PROVISIONED"
	NotProvisionedMessage = "Your workspace is still being set up. This data appears once setup finishes."
)

// IsMissingTable reports undefined_table (42P01) from either driver.
func IsMissingTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01"
	}
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "42P01"
}
