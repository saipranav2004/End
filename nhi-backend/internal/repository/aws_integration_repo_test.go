package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/database"
	modelsv1 "nhi-backend-api/internal/models/v1"
)

// Runs against a real nhi_db with migrations/global/001-003 applied:
//
//	NHI_TEST_DSN='postgres://postgres@localhost:5432/nhi_db?sslmode=disable' \
//	  go test ./internal/repository -run AWSIntegration -v
//
// The test creates two throwaway tenants with their own schemas (applying
// migrations/tenant/003_accounts_connection.sql to them), uses random account IDs,
// and drops all of it afterwards. Existing tenants and data are untouched.
func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func randomUUID(t *testing.T) string {
	h := randomHex(t, 16)
	return h[0:8] + "-" + h[8:12] + "-4" + h[13:16] + "-a" + h[17:20] + "-" + h[20:32]
}

func randomAccountID(t *testing.T) string {
	t.Helper()
	n, err := rand.Int(rand.Reader, big.NewInt(1e11))
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("9%011d", n.Int64())
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%.80s: %v", q, err)
	}
}

// applyTenantMigration runs the real tenant migration for schema.
func applyTenantMigration(t *testing.T, db *sql.DB, schema string) {
	t.Helper()
	// The scanner's accounts table as it exists in nhi_db (owned by the
	// discovery pipeline); the migration only adds the connection columns.
	mustExec(t, db, `CREATE SCHEMA `+`"`+schema+`"`)
	mustExec(t, db, `CREATE TABLE `+database.Table(schema, "accounts")+` (
		id uuid DEFAULT gen_random_uuid() PRIMARY KEY, tenant_id uuid NOT NULL, account_id text NOT NULL,
		is_default boolean DEFAULT false NOT NULL, created_at timestamptz DEFAULT now() NOT NULL,
		updated_at timestamptz DEFAULT now() NOT NULL, regions text[], client_role_arn varchar(512),
		client_aws_account_id varchar(12), external_id varchar(255), is_connected boolean DEFAULT false NOT NULL,
		account_status varchar(20) DEFAULT 'pending' NOT NULL, codecommit_enabled boolean DEFAULT false NOT NULL,
		github_enabled boolean DEFAULT false NOT NULL, CONSTRAINT account_account_id_unique UNIQUE (account_id))`)
	raw, err := os.ReadFile("../../migrations/tenant/003_accounts_connection.sql")
	if err != nil {
		t.Fatal(err)
	}
	sqlText := strings.ReplaceAll(string(raw), `:"schema"`, `"`+schema+`"`)
	sqlText = strings.ReplaceAll(sqlText, `:'schema'`, `'`+schema+`'`)
	if _, err := db.Exec(sqlText); err != nil {
		t.Fatalf("tenant migration: %v", err)
	}
}

func TestAWSIntegrationRepositoryAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("NHI_TEST_DSN")
	if dsn == "" {
		t.Skip("NHI_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	tag := randomHex(t, 5)
	tenantA, tenantB := randomUUID(t), randomUUID(t)
	schemaA, schemaB := "nhi_test_a_"+tag, "nhi_test_b_"+tag
	acct, acct2 := randomAccountID(t), randomAccountID(t)
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP SCHEMA IF EXISTS "` + schemaA + `" CASCADE`)
		_, _ = db.Exec(`DROP SCHEMA IF EXISTS "` + schemaB + `" CASCADE`)
		_, _ = db.Exec(`DELETE FROM nhi_global.tenants WHERE tenant_id IN ($1::uuid, $2::uuid)`, tenantA, tenantB)
	})
	mustExec(t, db, `INSERT INTO nhi_global.tenants (tenant_id, db_name, domain_name, collector_aws_account_id, collector_event_bridge_arn, collector_regions)
		VALUES ($1, $3, $5, '189160505560', 'arn:aws:events:ap-south-1:189160505560:event-bus/nhi-central-bus', '{us-east-1,eu-west-1}'),
		       ($2, $4, NULL, NULL, NULL, '{}')`, tenantA, tenantB, schemaA, schemaB, "t"+tag+".example.com")
	applyTenantMigration(t, db, schemaA)
	applyTenantMigration(t, db, schemaB)

	// The repository gets its pool the way production does.
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	pg, err := NewPostgresDB(&config.Config{Database: config.DatabaseConfig{
		Host: u.Hostname(), DatabaseName: strings.TrimPrefix(u.Path, "/"), User: u.User.Username(), Password: pass,
		Schema: config.DefaultGlobalSchema,
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.DB.Close() })
	repo := NewAWSIntegrationRepository(pg)

	// Collector values, with the generated External ID and the region list.
	s, err := repo.TenantSettings(ctx, tenantA)
	if err != nil || s == nil || s.AWSAccountID != "189160505560" || len(s.ExternalID) < 16 || !reflect.DeepEqual(s.Regions, []string{"us-east-1", "eu-west-1"}) || s.Domain != "t"+tag+".example.com" {
		t.Fatalf("TenantSettings(A) = %+v, %v", s, err)
	}
	if s, err := repo.TenantSettings(ctx, tenantB); err != nil || s == nil || s.AWSAccountID != "" || len(s.Regions) != 0 {
		t.Fatalf("TenantSettings(B) = %+v, %v", s, err)
	}

	scope := func(schema, account string) []string {
		rows, err := db.Query(`SELECT r FROM `+database.Table(schema, "accounts")+` a, unnest(a.regions) AS r WHERE a.account_id = $1`, account)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var r string
			_ = rows.Scan(&r)
			out = append(out, r)
		}
		sort.Strings(out)
		return out
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	rec := modelsv1.AWSAccountRecord{
		AccountID: acct, RoleARN: "arn:aws:iam::" + acct + ":role/NHIPlatformCollector",
		Regions: []string{"us-east-1"}, ExternalID: s.ExternalID, Status: modelsv1.AWSStatusPending,
		Checks:         []modelsv1.AWSCheck{{Key: "assume_role", Label: "Assume your role", State: "pass"}},
		LastVerifiedAt: now, LastVerifiedBy: "alice",
	}
	// Failing checks: pending, not queued. The row is saved (AssumeRole
	// passed, so the tenant owns the account) and carries 'global'.
	saved, queued, err := repo.SaveVerification(ctx, tenantA, schemaA, rec)
	if err != nil || queued || saved.Status != "pending" || saved.IsConnected || saved.ConnectedAt != nil ||
		!reflect.DeepEqual(saved.Regions, []string{"us-east-1"}) || !reflect.DeepEqual(scope(schemaA, acct), []string{"global", "us-east-1"}) {
		t.Fatalf("pending: %+v %v %v", saved, queued, err)
	}
	// Passing: connected, queued, in scope.
	rec.Status, rec.LastVerifiedAt = modelsv1.AWSStatusConnected, now.Add(time.Minute)
	saved, queued, err = repo.SaveVerification(ctx, tenantA, schemaA, rec)
	if err != nil || !queued || saved.Status != "connected" || !saved.IsConnected || saved.SyncStatus != "queued" {
		t.Fatalf("connected: %+v %v %v", saved, queued, err)
	}
	if got := scope(schemaA, acct); !reflect.DeepEqual(got, []string{"global", "us-east-1"}) {
		t.Fatalf("scope after connect: %v", got)
	}
	// Failing after connecting: action_needed, connected_at kept, no new queue.
	rec.Status, rec.LastVerifiedAt = modelsv1.AWSStatusPending, now.Add(2*time.Minute)
	saved, queued, err = repo.SaveVerification(ctx, tenantA, schemaA, rec)
	if err != nil || queued || saved.Status != "action_needed" || saved.IsConnected || saved.ConnectedAt == nil {
		t.Fatalf("action needed: %+v %v %v", saved, queued, err)
	}

	// Another tenant cannot claim it, even concurrently.
	if claimed, err := repo.AccountClaimedByOther(ctx, tenantB, acct); err != nil || !claimed {
		t.Fatalf("claimed by other: %v %v", claimed, err)
	}
	other := rec
	if _, _, err := repo.SaveVerification(ctx, tenantB, schemaB, other); !errors.Is(err, ErrAWSAccountClaimed) {
		t.Fatalf("second tenant save: %v", err)
	}
	var wg sync.WaitGroup
	wins := make(chan string, 2)
	race := modelsv1.AWSAccountRecord{AccountID: acct2, RoleARN: "arn:aws:iam::" + acct2 + ":role/R", Regions: []string{"us-east-1"},
		ExternalID: "ext-0123456789abcdef", Status: modelsv1.AWSStatusConnected, LastVerifiedAt: now}
	for _, tc := range [][2]string{{tenantA, schemaA}, {tenantB, schemaB}} {
		wg.Add(1)
		go func(tenant, schema string) {
			defer wg.Done()
			if _, _, err := repo.SaveVerification(ctx, tenant, schema, race); err == nil {
				wins <- tenant
			}
		}(tc[0], tc[1])
	}
	wg.Wait()
	close(wins)
	if n := len(wins); n != 1 {
		t.Fatalf("%d tenants saved the same account concurrently, want exactly 1", n)
	}

	// Regions follow into scope (one row per account); the scanner's "global" region is kept.
	mustExec(t, db, `UPDATE `+database.Table(schemaA, "accounts")+` SET regions = array_append(regions, 'global') WHERE tenant_id = $1 AND account_id = $2`, tenantA, acct)
	if _, err := repo.UpdateRegions(ctx, tenantA, schemaA, acct, []string{"eu-west-1"}); err != nil {
		t.Fatal(err)
	}
	if got := scope(schemaA, acct); !reflect.DeepEqual(got, []string{"eu-west-1", "global"}) {
		t.Fatalf("scope after regions: %v", got)
	}
	if _, err := repo.UpdateRegions(ctx, tenantB, schemaB, acct, []string{"eu-west-1"}); !errors.Is(err, ErrAWSAccountNotFound) {
		t.Fatalf("other tenant's account: %v", err)
	}

	// Exposure state.
	got, err := repo.SaveExposure(ctx, schemaA, acct, ExposureUpdate{
		Actor: "alice", At: now,
		CodeCommit: &ExposurePlatformUpdate{Connected: true, Checks: []modelsv1.AWSCheck{{Key: "codecommit_list", State: "pass"}}},
		GitHub:     &ExposurePlatformUpdate{Connected: false, Checks: []modelsv1.AWSCheck{{Key: "github_secret", State: "fail"}}, SecretARN: "arn:aws:secretsmanager:us-east-1:" + acct + ":secret:nhi/github-x"},
	})
	if err != nil || !got.CodeCommitConnected || got.GitHubConnected || got.GitHubSecretARN == "" || got.CodeCommitVerified == nil || len(got.GitHubChecks) != 1 {
		t.Fatalf("exposure: %+v %v", got, err)
	}
	got, err = repo.SaveExposure(ctx, schemaA, acct, ExposureUpdate{Actor: "bob", At: now, GitHub: &ExposurePlatformUpdate{}})
	if err != nil || got.GitHubSecretARN != "" || got.GitHubVerified != nil || !got.CodeCommitConnected || got.ExposureUpdatedBy != "bob" {
		t.Fatalf("github off leaves codecommit: %+v %v", got, err)
	}

	// Sync: not connected now (action_needed) -> refused; connected -> queued once.
	if _, _, err := repo.RequestSync(ctx, schemaA, acct, "bob", now); !errors.Is(err, ErrAWSAccountNotConnected) {
		t.Fatalf("sync while not connected: %v", err)
	}
	rec.Status, rec.Regions = modelsv1.AWSStatusConnected, []string{"eu-west-1"}
	if _, _, err := repo.SaveVerification(ctx, tenantA, schemaA, rec); err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `UPDATE `+database.Table(schemaA, "accounts")+` SET sync_status = 'succeeded' WHERE account_id = $1`, acct)
	if r, already, err := repo.RequestSync(ctx, schemaA, acct, "bob", now); err != nil || already || r.SyncStatus != "queued" || r.SyncRequestedBy != "bob" {
		t.Fatalf("sync: %+v %v %v", r, already, err)
	}
	if _, already, _ := repo.RequestSync(ctx, schemaA, acct, "carol", now); !already {
		t.Fatal("second sync should report already queued")
	}

	// List and delete.
	if list, err := repo.ListAccounts(ctx, schemaA); err != nil || len(list) == 0 {
		t.Fatalf("list: %v %v", list, err)
	}
	if err := repo.DeleteAccount(ctx, tenantA, schemaA, acct); err != nil {
		t.Fatal(err)
	}
	if got := scope(schemaA, acct); len(got) != 0 {
		t.Fatalf("scope after delete: %v", got)
	}
	if err := repo.DeleteAccount(ctx, tenantA, schemaA, acct); !errors.Is(err, ErrAWSAccountNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if claimed, _ := repo.AccountClaimedByOther(ctx, tenantB, acct); claimed {
		t.Fatal("deleted account still claimed")
	}
}
