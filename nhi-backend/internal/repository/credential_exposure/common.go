package credential_exposure

import (
	"database/sql"
	"time"
)

func dashboardTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05+00:00")
}

func stringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	x := v.String
	return &x
}

func nullableIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	x := int(v.Int64)
	return &x
}

func floatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	x := v.Float64
	return &x
}

func strPtr(v sql.NullString) *string {
	return stringPtr(v)
}

func intPtr(v sql.NullInt64) *int {
	return nullableIntPtr(v)
}
