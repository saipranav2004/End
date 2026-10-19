package v1

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"

	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/database"
	repov1 "nhi-backend-api/internal/repository/v1"
)

// Read-only check of the lookup queries and the resolver against a real
// tenant database. It skips unless NHI_TEST_TENANT_DB (tenant schema name) and
// NHI_TEST_TENANT_ID are set, so it never runs by accident.
func TestIntegrationIdentityResolverAgainstTenantDB(t *testing.T) {
	dbName, tenantID := os.Getenv("NHI_TEST_TENANT_DB"), os.Getenv("NHI_TEST_TENANT_ID")
	if dbName == "" || tenantID == "" {
		t.Skip("NHI_TEST_TENANT_DB / NHI_TEST_TENANT_ID not set")
	}
	_ = godotenv.Load("../../../.env")
	t.Setenv("NHI_CONFIG_FILE", "../../../configs/config.yaml")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	db, err := sql.Open("pgx", cfg.Database.GetConnectionString())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	repo := repov1.NewIdentityLookupRepository()

	accounts, err := repo.TenantAccountIDs(ctx, db, database.Table("", dbName), tenantID)
	if err != nil || len(accounts) == 0 {
		t.Fatalf("TenantAccountIDs = %v, err %v", accounts, err)
	}
	t.Logf("tenant accounts: %v", accounts)

	l, err := repo.Load(ctx, db, database.Table("", dbName), accounts)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Logf("%d identities, %d credentials, %d bindings", len(l.Identities), len(l.Credentials), len(l.Bindings))
	if len(l.Identities) == 0 {
		t.Fatal("expected identities for the tenant accounts")
	}

	r := NewIdentityResolver(l)
	for _, i := range l.Identities {
		t.Logf("  identity %s (%s) terminated=%v", i.ARN, i.Type, i.Terminated)
		if m := r.Resolve(ResolveInput{CallerARN: i.ARN}); m.IdentityID != i.ID {
			t.Fatalf("identity %s resolved to %+v, want id %s", i.ARN, m, i.ID)
		}
	}
	// A service-name caller (as seen in the CloudTrail data) must stay unmatched.
	if m := r.Resolve(ResolveInput{CallerARN: "resource-explorer-2.amazonaws.com", SourceIP: "resource-explorer-2.amazonaws.com"}); m.Matched() {
		t.Fatalf("service caller matched: %+v", m)
	}
}
