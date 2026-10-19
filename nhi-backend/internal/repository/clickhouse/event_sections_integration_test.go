package clickhouse

import (
	"context"
	"testing"
)

// Runs every Identity Event Graph query against the real ClickHouse (read-only;
// skips when .env has no NHI_CLICKHOUSE_HOST). Uses the dev data described in
// integration_test.go.
func TestIntegrationEventSections(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()
	sc := Scope{Accounts: []string{devAccount}}
	sub := NewSubject(devUser, devKey, devService)
	var f EventFilter

	page, err := c.EventsPage(ctx, sub, sc, f, 100, 0)
	if err != nil || len(page) == 0 {
		t.Fatalf("EventsPage = %d rows, err %v", len(page), err)
	}
	total, err := c.CountFiltered(ctx, sub, sc, f)
	if err != nil || total < uint64(len(page)) {
		t.Fatalf("CountFiltered = %d, err %v (page has %d)", total, err, len(page))
	}
	if ev, err := c.EventByID(ctx, sub, sc, page[0].EventID); err != nil || ev == nil || ev.EventID != page[0].EventID {
		t.Fatalf("EventByID = %+v, err %v", ev, err)
	}
	if ev, err := c.EventByID(ctx, NewSubject("arn:aws:iam::1:user/nobody"), sc, page[0].EventID); err != nil || ev != nil {
		t.Fatalf("EventByID with foreign subject = %+v, err %v; want nil", ev, err)
	}
	if ev, err := c.EventByID(ctx, sub, Scope{Accounts: []string{"000000000000"}}, page[0].EventID); err != nil || ev != nil {
		t.Fatalf("EventByID outside tenant accounts = %+v, err %v; want nil", ev, err)
	}

	// Filters: each must run and never return more than the unfiltered set.
	for name, ff := range map[string]EventFilter{
		"action":     {Action: page[0].EventName},
		"resource":   {Resource: "role"},
		"ip":         {SourceIP: page[0].SourceIP},
		"credential": {Credential: devUser},
		"success":    {Result: "success"},
		"error":      {Result: "error"},
		"read":       {ReadWrite: "read"},
		"write":      {ReadWrite: "write"},
	} {
		n, err := c.CountFiltered(ctx, sub, sc, ff)
		if err != nil || n > total {
			t.Fatalf("filter %s: count %d (total %d), err %v", name, n, total, err)
		}
	}
	if _, err := c.CountFiltered(ctx, sub, Scope{Accounts: sc.Accounts, Service: "sts"}, f); err != nil {
		t.Fatalf("service filter: %v", err)
	}

	if rows, err := c.CredentialUsage(ctx, sub, sc, f); err != nil || len(rows) == 0 {
		t.Fatalf("CredentialUsage = %d rows, err %v", len(rows), err)
	} else {
		t.Logf("credential usage: %+v", rows[0])
	}
	if rows, n, err := c.ResourceAccess(ctx, sub, sc, f); err != nil || len(rows) == 0 || n == 0 {
		t.Fatalf("ResourceAccess = %d rows, total %d, err %v", len(rows), n, err)
	}
	if cells, err := c.Heatmap(ctx, sub, sc, f); err != nil || len(cells) == 0 {
		t.Fatalf("Heatmap = %d cells, err %v", len(cells), err)
	}
	if rows, trunc, err := c.SessionEvents(ctx, sub, sc, f); err != nil || len(rows) == 0 || trunc {
		t.Fatalf("SessionEvents = %d rows, truncated %v, err %v", len(rows), trunc, err)
	}
	sum, err := c.ErrorSummary(ctx, sub, sc, f)
	if err != nil || sum.TotalErrors == 0 {
		t.Fatalf("ErrorSummary = %+v, err %v", sum, err)
	}
	t.Logf("errors: total %d codes %+v denied-action %+v denied-resource %+v", sum.TotalErrors, sum.ByCode, sum.MostDeniedAct, sum.MostDeniedRes)
	if _, _, err := c.CrossAccount(ctx, sub, sc, f, "111111111111"); err != nil {
		t.Fatalf("CrossAccount: %v", err)
	}
	if rows, n, err := c.IPStats(ctx, sub, sc, f); err != nil || len(rows) == 0 || n == 0 {
		t.Fatalf("IPStats = %d rows, unique %d, err %v", len(rows), n, err)
	}
	// No subject keys or no accounts must run nothing.
	if rows, _ := c.EventsPage(ctx, NewSubject(), sc, f, 10, 0); rows != nil {
		t.Fatal("empty subject must return nothing")
	}
	if rows, _ := c.EventsPage(ctx, sub, Scope{}, f, 10, 0); rows != nil {
		t.Fatal("empty accounts must return nothing")
	}
}
