package v1

import (
	"context"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

const (
	activityCacheTTL  = 5 * time.Minute
	activityCacheSize = 100000
)

// ttlCache is a small mutex-guarded cache. When it grows past max it is
// emptied rather than evicted entry by entry: the data is cheap to refetch.
type ttlCache[V any] struct {
	mu  sync.Mutex
	ttl time.Duration
	max int
	now func() time.Time
	m   map[string]ttlEntry[V]
}

type ttlEntry[V any] struct {
	v       V
	expires time.Time
}

func newTTLCache[V any](ttl time.Duration, max int, now func() time.Time) *ttlCache[V] {
	return &ttlCache[V]{ttl: ttl, max: max, now: now, m: map[string]ttlEntry[V]{}}
}

func (c *ttlCache[V]) get(k string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || !c.now().Before(e.expires) {
		delete(c.m, k)
		var zero V
		return zero, false
	}
	return e.v, true
}

func (c *ttlCache[V]) set(k string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.max {
		c.m = map[string]ttlEntry[V]{}
	}
	c.m[k] = ttlEntry[V]{v: v, expires: c.now().Add(c.ttl)}
}

// seenEntry caches "last event for this key", including the negative answer.
type seenEntry struct {
	t     time.Time
	found bool
}

// ActivityService hands out per-request ActivitySources backed by ClickHouse.
// Last-seen times, creators and tenant account lists are cached for a few
// minutes because they change slowly and are requested on every list page.
type ActivityService struct {
	ch       *chrepo.Repository
	lookup   *repov1.IdentityLookupRepository
	seen     *ttlCache[seenEntry]
	creators *ttlCache[string]
	accounts *ttlCache[[]string]
}

// NewActivityService wraps the ClickHouse repository. ch may be nil (or a
// nil *Repository from chrepo.New), which yields sources that report
// "unavailable".
func NewActivityService(ch *chrepo.Repository, lookup *repov1.IdentityLookupRepository) *ActivityService {
	now := time.Now
	return &ActivityService{
		ch:       ch,
		lookup:   lookup,
		seen:     newTTLCache[seenEntry](activityCacheTTL, activityCacheSize, now),
		creators: newTTLCache[string](activityCacheTTL, activityCacheSize, now),
		accounts: newTTLCache[[]string](activityCacheTTL, 1000, now),
	}
}

// ForRequest returns the ActivitySource for one request. It is never nil.
func (a *ActivityService) ForRequest(resolved *scope.DataScope) *RequestActivity {
	r := &RequestActivity{svc: a, resolved: resolved}
	if resolved != nil {
		r.tenantID = resolved.TenantID
	}
	return r
}

// RequestActivity implements repov1.ActivitySource for one request.
type RequestActivity struct {
	svc      *ActivityService
	resolved *scope.DataScope
	tenantID string
	degraded atomic.Bool
}

var _ repov1.ActivitySource = (*RequestActivity)(nil)

func (r *RequestActivity) Available() bool {
	return r != nil && r.svc != nil && r.svc.ch.Enabled() && r.resolved != nil && r.resolved.DB != nil
}

func (r *RequestActivity) Degraded() bool { return r != nil && r.degraded.Load() }

func (r *RequestActivity) fail(what string, err error) {
	msg := err.Error()
	if len(msg) > 300 { // ClickHouse errors echo the whole query
		msg = msg[:300] + "..."
	}
	log.Printf("clickhouse %s: %s (serving Postgres data only)", what, msg)
	r.degraded.Store(true)
}

// tenantAccounts returns every account of the tenant, the isolation boundary
// for ClickHouse queries.
func (r *RequestActivity) tenantAccounts(ctx context.Context) []string {
	if !r.Available() {
		return nil
	}
	if v, ok := r.svc.accounts.get(r.tenantID); ok {
		return v
	}
	accounts, err := r.svc.lookup.TenantAccountIDs(ctx, r.resolved.DB, r.resolved.QSchema(), r.tenantID)
	if err != nil {
		r.fail("tenant accounts", err)
		return nil
	}
	r.svc.accounts.set(r.tenantID, accounts)
	return accounts
}

func (r *RequestActivity) scopeFor(ctx context.Context) (chrepo.Scope, bool) {
	accounts := r.tenantAccounts(ctx)
	return chrepo.Scope{Accounts: accounts}, len(accounts) > 0
}

func (r *RequestActivity) LastSeen(ctx context.Context, keys []string) map[string]time.Time {
	out := map[string]time.Time{}
	keys = dedupeKeys(keys)
	if len(keys) == 0 || !r.Available() {
		return out
	}
	var miss []string
	for _, k := range keys {
		if e, ok := r.svc.seen.get(r.tenantID + "|" + k); ok {
			if e.found {
				out[k] = e.t
			}
			continue
		}
		miss = append(miss, k)
	}
	if len(miss) == 0 {
		return out
	}
	sc, ok := r.scopeFor(ctx)
	if !ok {
		return out
	}
	found, err := r.svc.ch.LastSeenByKeys(ctx, miss, sc)
	if err != nil {
		r.fail("last seen", err)
		return out
	}
	for _, k := range miss {
		t, ok := found[k]
		r.svc.seen.set(r.tenantID+"|"+k, seenEntry{t: t, found: ok})
		if ok {
			out[k] = t
		}
	}
	return out
}

func (r *RequestActivity) Creators(ctx context.Context, arns []string) map[string]string {
	out := map[string]string{}
	arns = dedupeKeys(arns)
	if len(arns) == 0 || !r.Available() {
		return out
	}
	var miss []string
	for _, a := range arns {
		if v, ok := r.svc.creators.get(r.tenantID + "|" + a); ok {
			if v != "" {
				out[a] = v
			}
			continue
		}
		miss = append(miss, a)
	}
	if len(miss) == 0 {
		return out
	}
	accounts := r.tenantAccounts(ctx)
	if len(accounts) == 0 {
		return out
	}
	found, err := r.svc.ch.CreatorsByARNs(ctx, miss, accounts)
	if err != nil {
		r.fail("creators", err)
		return out
	}
	for _, a := range miss {
		r.svc.creators.set(r.tenantID+"|"+a, found[a])
		if v := found[a]; v != "" {
			out[a] = v
		}
	}
	return out
}

func (r *RequestActivity) UsedActionSets(ctx context.Context, keys []string, since time.Time) map[string][]chrepo.UsedAction {
	keys = dedupeKeys(keys)
	if len(keys) == 0 || !r.Available() {
		return nil
	}
	accounts := r.tenantAccounts(ctx)
	if len(accounts) == 0 {
		return nil
	}
	out, err := r.svc.ch.UsedActionsByKeys(ctx, keys, accounts, since)
	if err != nil {
		r.fail("used actions", err)
		return nil
	}
	return out
}

func (r *RequestActivity) UsedPermissions(ctx context.Context, keys []string, since time.Time) []chrepo.UsedPermission {
	keys = dedupeKeys(keys)
	if len(keys) == 0 || !r.Available() {
		return nil
	}
	sc, ok := r.scopeFor(ctx)
	if !ok {
		return nil
	}
	sc.Start = since
	out, err := r.svc.ch.UsedPermissions(ctx, chrepo.NewSubject(keys...), sc)
	if err != nil {
		r.fail("used permissions", err)
		return nil
	}
	return out
}

func (r *RequestActivity) AccessEdges(ctx context.Context, keys []string, since, until time.Time) []chrepo.AccessEdge {
	keys = dedupeKeys(keys)
	if len(keys) == 0 || !r.Available() {
		return nil
	}
	sc, ok := r.scopeFor(ctx)
	if !ok {
		return nil
	}
	sc.Start = since
	if !until.IsZero() {
		sc.End = until
	}
	out, err := r.svc.ch.AccessEdges(ctx, chrepo.NewSubject(keys...), sc)
	if err != nil {
		r.fail("access edges", err)
		return nil
	}
	return out
}

func (r *RequestActivity) AccountVolume(ctx context.Context, accounts []string, since time.Time) ([]chrepo.VolumePoint, bool) {
	if !r.Available() {
		return nil, false
	}
	// Only the tenant's own accounts, whatever the caller passed.
	tenant := map[string]bool{}
	for _, a := range r.tenantAccounts(ctx) {
		tenant[a] = true
	}
	var mine []string
	for _, a := range accounts {
		if tenant[a] {
			mine = append(mine, a)
		}
	}
	if len(mine) == 0 {
		return nil, false
	}
	out, err := r.svc.ch.AccountVolume(ctx, chrepo.Scope{Accounts: mine, Start: since})
	if err != nil {
		r.fail("account volume", err)
		return nil, false
	}
	return out, true
}

func dedupeKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

// evidenceWindow is how far before a scan a creating or deleting call is
// looked for (scans normally run far more often than this).
const evidenceWindow = 30 * 24 * time.Hour

// changeEvidenceBudget is the most time evidence lookups may add to a request.
const changeEvidenceBudget = 3 * time.Second

// AttachChangeEvidence sets cloudtrail_evidence on CREATED and DELETED
// identity and credential changes whose call CloudTrail recorded in the 30
// days up to the scan. Best effort: without ClickHouse nothing is attached.
func (r *RequestActivity) AttachChangeEvidence(ctx context.Context, changes []models.ScanChange) {
	if len(changes) == 0 || !r.Available() {
		return
	}
	// Evidence is a nicety: a slow ClickHouse must not hold up the changes.
	ctx, cancel := context.WithTimeout(ctx, changeEvidenceBudget)
	defer cancel()
	sc, ok := r.scopeFor(ctx)
	if !ok {
		return
	}
	type want struct {
		arns     []string
		from, to time.Time
	}
	var created, deleted want
	add := func(w *want, ch models.ScanChange) {
		at := ch.DetectedAt
		if ch.ScanAt != nil {
			at = *ch.ScanAt
		}
		from, to := at.Add(-evidenceWindow), at.Add(5*time.Minute)
		if w.from.IsZero() || from.Before(w.from) {
			w.from = from
		}
		if to.After(w.to) {
			w.to = to
		}
		w.arns = append(w.arns, ch.EntityARN)
	}
	for _, ch := range changes {
		if ch.EntityType != models.EntityIdentity && ch.EntityType != models.EntityCredential {
			continue
		}
		switch ch.ChangeType {
		case models.ChangeCreated:
			add(&created, ch)
		case models.ChangeDeleted:
			add(&deleted, ch)
		}
	}
	lookup := func(w want, events []string) map[string]chrepo.ChangeEvidence {
		if len(w.arns) == 0 {
			return nil
		}
		found, err := r.svc.ch.ChangeEvidenceByARNs(ctx, w.arns, events, sc.Accounts, w.from, w.to)
		if err != nil {
			r.fail("change evidence", err)
			return nil
		}
		return found
	}
	byCreated := lookup(created, chrepo.CredentialCreationEvents)
	byDeleted := lookup(deleted, chrepo.DeletionEvents)
	for i := range changes {
		ch := &changes[i]
		var found map[string]chrepo.ChangeEvidence
		switch {
		case ch.EntityType != models.EntityIdentity && ch.EntityType != models.EntityCredential:
			continue
		case ch.ChangeType == models.ChangeCreated:
			found = byCreated
		case ch.ChangeType == models.ChangeDeleted:
			found = byDeleted
		default:
			continue
		}
		ev, ok := found[ch.EntityARN]
		if !ok {
			continue
		}
		at := ch.DetectedAt
		if ch.ScanAt != nil {
			at = *ch.ScanAt
		}
		if ev.EventTime.Before(at.Add(-evidenceWindow)) || ev.EventTime.After(at.Add(5*time.Minute)) {
			continue
		}
		ch.CloudTrailEvidence = &models.CloudTrailEvidence{CallerARN: ev.CallerARN, EventName: ev.EventName, SourceIP: ev.SourceIP, EventTime: ev.EventTime}
	}
}
