package credential_exposure

import (
	"database/sql"
	"time"
)

func dashboardTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05+00:00")
}

// scannerTime returns a scanner timestamp as ISO 8601 with an explicit
// offset. Rows written before the scanner's storage move carry no zone
// ("2026-09-18 12:17:39", UTC per the scanner's guide); left as is, a browser
// would read them as local time. Anything else is passed through unchanged.
func scannerTime(s string) string {
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return dashboardTimestamp(t)
		}
	}
	return s
}

func scannerTimePtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := scannerTime(*s)
	return &v
}

// scannerTimeOrder sorts a text timestamp column chronologically whatever its
// offset style; rows that are not a timestamp sort last.
func scannerTimeOrder(col string) string {
	v := `(` + col + ` || CASE WHEN ` + col + ` ~ '(Z|[+-]\d\d(:?\d\d)?)$' THEN '' ELSE '+00' END)`
	return `(CASE WHEN pg_input_is_valid(` + v + `, 'timestamptz') THEN ` + v + `::timestamptz END) DESC NULLS LAST`
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
