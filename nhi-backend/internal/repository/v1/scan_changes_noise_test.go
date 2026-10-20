package v1

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// notScannerNoise must hide only a type_attributes change whose sole
// difference is scan_errors, and must never fail on odd details.
func TestNotScannerNoiseAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("NHI_TEST_DSN")
	if dsn == "" {
		t.Skip("NHI_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fc := func(field, old, new string) string {
		return `{"fields_changed":[{"field":"` + field + `","old_value":` + old + `,"new_value":` + new + `}]}`
	}
	cases := []struct {
		name, changeType, details string
		kept                      bool
	}{
		{"scan_errors only", "MODIFIED", fc("type_attributes", `"{\"a\":1,\"scan_errors\":[\"x RequestID 1\"]}"`, `"{\"a\":1,\"scan_errors\":[\"x RequestID 2\"]}"`), false},
		{"real attribute change", "MODIFIED", fc("type_attributes", `"{\"a\":1,\"scan_errors\":[]}"`, `"{\"a\":2,\"scan_errors\":[]}"`), true},
		{"not JSON", "MODIFIED", fc("type_attributes", `"not json"`, `"{\"a\":1}"`), true},
		{"JSON scalar", "MODIFIED", fc("type_attributes", `"1"`, `"2"`), true},
		{"other field", "MODIFIED", fc("status", `"active"`, `"inactive"`), true},
		{"created", "CREATED", fc("type_attributes", `"{\"scan_errors\":[1]}"`, `"{\"scan_errors\":[2]}"`), true},
		{"no details", "MODIFIED", `null`, true},
		{"fields_changed not an array", "MODIFIED", `{"fields_changed":{}}`, true},
	}
	// A real table, as in production: with literal parameters the planner
	// would constant-fold the casts inside the CASE and defeat the guards.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TEMP TABLE noise_case (name text, change_type text, details jsonb) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		if _, err := tx.Exec(`INSERT INTO noise_case VALUES ($1, $2, $3::jsonb)`, tc.name, tc.changeType, tc.details); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	for _, tc := range cases {
		var kept bool
		if err := tx.QueryRow(`SELECT `+notScannerNoise+` FROM noise_case c WHERE c.name = $1`, tc.name).Scan(&kept); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if kept != tc.kept {
			t.Errorf("%s: kept = %v, want %v", tc.name, kept, tc.kept)
		}
	}
}
