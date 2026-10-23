package clickhouse

import (
	"context"
	"testing"
)

func TestTableFor(t *testing.T) {
	perTenant := &Client{table: "cloudtrail_events", suffix: "_clickhouse"}
	ctx := WithTenant(context.Background(), "bimasugam")
	if got := perTenant.TableFor(ctx); got != "`bimasugam_clickhouse`.`cloudtrail_events`" {
		t.Errorf("per-tenant: %s", got)
	}
	if got := perTenant.TableFor(context.Background()); got != "cloudtrail_events" {
		t.Errorf("no tenant on the context must not guess a database: %s", got)
	}
	fixed := &Client{table: "cloudtrail_events"}
	if got := fixed.TableFor(ctx); got != "cloudtrail_events" {
		t.Errorf("fixed database ignores the tenant: %s", got)
	}
}
