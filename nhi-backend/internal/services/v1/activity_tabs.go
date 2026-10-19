package v1

import (
	"context"
	"log"
	"sync"
	"time"

	models "nhi-backend-api/internal/models/v1"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/scope"
)

const (
	activityPageSize    = 50  // default events per page
	maxActivityPageSize = 100 // events per page, upper bound (1000 per request overall)
	activityTopN        = 10
	activityPeakHours   = 5
)

// activityFetch is everything the activity tabs need from ClickHouse.
type activityFetch struct {
	volume  []chrepo.VolumePoint
	events  []chrepo.Event
	total   uint64
	errors  []chrepo.Event
	rows    []chrepo.AnalyticsRow
	callers []chrepo.CallerUsage // credential tab only
}

// fetchActivity runs the activity queries concurrently over the given
// tenant accounts and the requested window. The first error wins and is
// returned with a nil result; callers then answer with an empty,
// "unavailable" tab.
func (a *ActivityService) fetchActivity(ctx context.Context, accounts []string, keys []string, tr *ResolvedTimeRange, p activityPaging, service string, withCallers bool, credentialARN string) (*activityFetch, error) {
	sc := chrepo.Scope{Accounts: accounts, Start: tr.Start, End: tr.End, Service: service}
	sub := chrepo.NewSubject(keys...)
	out := &activityFetch{}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}()
	}
	ch := a.ch
	run(func() (err error) { out.volume, err = ch.VolumeChart(ctx, sub, sc); return })
	run(func() (err error) { out.events, err = ch.ActivityEvents(ctx, sub, sc, p.size, p.offset()); return })
	run(func() (err error) { out.total, err = ch.CountEvents(ctx, sub, sc); return })
	run(func() (err error) { out.errors, err = ch.ErrorEvents(ctx, sub, sc, activityPageSize); return })
	run(func() (err error) { out.rows, err = ch.AnalyticsRows(ctx, sub, sc); return })
	if withCallers {
		run(func() (err error) { out.callers, err = ch.IdentitiesUsingCredential(ctx, credentialARN, sc); return })
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// activityPaging is the requested page of the events table.
type activityPaging struct{ page, size int }

func (p activityPaging) offset() int { return (p.page - 1) * p.size }

// normalizePaging applies the defaults and bounds (page >= 1, 1..100 per page).
func normalizePaging(page, size int) activityPaging {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = activityPageSize
	}
	if size > maxActivityPageSize {
		size = maxActivityPageSize
	}
	return activityPaging{page: page, size: size}
}

// loadLookup reads the identity/credential map used to name events. It runs
// next to the ClickHouse fetch; a failure only costs the friendly names.
func (a *ActivityService) loadLookup(ctx context.Context, resolved *scope.DataScope, accounts []string) *repov1.IdentityLookup {
	l, err := a.lookup.Load(ctx, resolved.DB, resolved.QSchema(), accounts)
	if err != nil {
		log.Printf("identity lookup for activity events: %v (showing raw callers)", err)
		return nil
	}
	return l
}

// IdentityActivity builds tab=activity for an identity from ClickHouse. It
// never fails: when CloudTrail cannot be read the tab is empty and
// activity_data_available is false.
func (a *ActivityService) IdentityActivity(ctx context.Context, resolved *scope.DataScope, subj *repov1.ActivitySubject, tr *ResolvedTimeRange, service string, page, pageSize int) *models.IdentityActivityResponse {
	p := normalizePaging(page, pageSize)
	resp := emptyIdentityActivity(tr, p)
	req := a.ForRequest(resolved)
	if !req.Available() || subj == nil {
		return resp
	}
	accounts := req.tenantAccounts(ctx)
	if len(accounts) == 0 {
		resp.ActivityDataAvailable = !req.Degraded()
		return resp
	}

	var lookup *repov1.IdentityLookup
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); lookup = a.loadLookup(ctx, resolved, accounts) }()
	f, err := a.fetchActivity(ctx, accounts, subj.Keys, tr, p, service, false, "")
	wg.Wait()
	if err != nil {
		req.fail("identity activity", err)
		return resp
	}
	resp = identityActivityFrom(f, tr, p, newEventEnricher(lookup))
	resp.ActivityDataAvailable = !req.Degraded()
	return resp
}

// CredentialActivity builds tab=activity for a credential, including which
// identities used it (callers resolved back to Postgres identities).
func (a *ActivityService) CredentialActivity(ctx context.Context, resolved *scope.DataScope, subj *repov1.ActivitySubject, tr *ResolvedTimeRange, service string, page, pageSize int) *models.ActivityResponse {
	p := normalizePaging(page, pageSize)
	resp := emptyCredentialActivity(tr, p)
	req := a.ForRequest(resolved)
	if !req.Available() || subj == nil {
		return resp
	}
	accounts := req.tenantAccounts(ctx)
	if len(accounts) == 0 {
		resp.ActivityDataAvailable = !req.Degraded()
		return resp
	}

	var lookup *repov1.IdentityLookup
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); lookup = a.loadLookup(ctx, resolved, accounts) }()
	f, err := a.fetchActivity(ctx, accounts, subj.Keys, tr, p, service, true, subj.ARN)
	wg.Wait()
	if err != nil {
		req.fail("credential activity", err)
		return resp
	}
	enr := newEventEnricher(lookup)
	resp = credentialActivityFrom(f, tr, p, enr)
	resp.IdentitiesUsingThisCredential = resolveCallers(f.callers, subj.ARN, lookup)
	resp.ActivityDataAvailable = !req.Degraded()
	return resp
}

// resolveCallers maps CloudTrail callers of a credential back to identities.
// Unmatched callers are kept and identified by their raw ARN.
func resolveCallers(callers []chrepo.CallerUsage, credentialARN string, lookup *repov1.IdentityLookup) []models.UsingIdentity {
	out := make([]models.UsingIdentity, 0, len(callers))
	resolver := NewIdentityResolver(lookup)
	byID := map[string]repov1.LookupIdentity{}
	if lookup != nil {
		for _, i := range lookup.Identities {
			byID[i.ID] = i
		}
	}
	for _, c := range callers {
		out = append(out, usingIdentityFrom(c, resolver.Resolve(ResolveInput{
			CallerARN: c.CallerARN, SessionIssuerARN: credentialARN, SourceIP: c.SourceIP,
		}), byID))
	}
	return out
}

// eventEnricher names the principal behind each CloudTrail event using the
// resolver over a lookup map that was loaded once for the request.
type eventEnricher struct {
	resolver *IdentityResolver
	creds    map[string]repov1.LookupCredential
	idents   map[string]repov1.LookupIdentity
}

func newEventEnricher(l *repov1.IdentityLookup) *eventEnricher {
	if l == nil {
		return nil
	}
	e := &eventEnricher{
		resolver: NewIdentityResolver(l),
		creds:    make(map[string]repov1.LookupCredential, len(l.Credentials)),
		idents:   make(map[string]repov1.LookupIdentity, len(l.Identities)),
	}
	for _, c := range l.Credentials {
		e.creds[c.ID] = c
	}
	for _, i := range l.Identities {
		e.idents[i.ID] = i
	}
	return e
}

// principal returns who made the call. A nil enricher (lookup unavailable)
// still reports the raw caller; unmatched events are never dropped.
func (e *eventEnricher) principal(ev chrepo.Event) models.EventPrincipal {
	p := models.EventPrincipal{CallerARN: ev.CallerARN, CallerType: strPtr(ev.CallerType)}
	if e == nil {
		return p
	}
	m := e.resolver.Resolve(ResolveInput{
		CallerARN: ev.CallerARN, SessionIssuerARN: ev.SessionIssuerARN,
		AccessKeyID: ev.AccessKeyID, SourceIP: ev.SourceIP,
	})
	if c, ok := e.creds[m.CredentialID]; ok && m.CredentialID != "" {
		id, name := c.ID, c.Name
		p.CredentialID, p.CredentialName = &id, &name
		p.Terminated = m.CredentialTerminated
	}
	if i, ok := e.idents[m.IdentityID]; ok && m.IdentityID != "" {
		id, name := i.ID, i.Name
		p.IdentityID, p.IdentityName = &id, &name
		p.Terminated = m.IdentityTerminated
	}
	return p
}

func usingIdentityFrom(c chrepo.CallerUsage, m Match, byID map[string]repov1.LookupIdentity) models.UsingIdentity {
	u := models.UsingIdentity{
		CallerARN:      c.CallerARN,
		Name:           arnLastSegment(c.CallerARN),
		IdentityType:   "unknown",
		SourceIP:       strPtr(c.SourceIP),
		LastAction:     strPtr(c.LastAction),
		LastActionTime: timePtr(c.LastActionTime),
	}
	if i, ok := byID[m.IdentityID]; ok && m.IdentityID != "" {
		u.IdentityID, u.Name, u.IdentityType, u.Terminated = i.ID, i.Name, i.Type, m.IdentityTerminated
	}
	return u
}

// ── pure mapping from fetched rows to API shapes ───────────────────────────

func emptyIdentityActivity(tr *ResolvedTimeRange, p activityPaging) *models.IdentityActivityResponse {
	return &models.IdentityActivityResponse{
		Tab: "activity", TimeRange: tr.Label,
		VolumeChart:  []models.VolumePoint{},
		RecentEvents: []models.IdentityRecentEvent{},
		ErrorEvents:  []models.IdentityErrorEvent{},
		Analytics: models.IdentityActivityAnalytics{
			SourceIPs: []models.IPPct{}, ActiveRegions: []models.RegionCnt{},
			TopServices: []models.ServicePct{}, TopActions: []models.IdentityActionCount{},
			PeakHours: []models.IdentityHourCount{}, UsagePattern: "Dormant",
		},
		EventsPagination: models.EventsPagination{Page: p.page, PageSize: p.size},
	}
}

func emptyCredentialActivity(tr *ResolvedTimeRange, p activityPaging) *models.ActivityResponse {
	return &models.ActivityResponse{
		Tab: "activity", TimeRange: tr.Label,
		VolumeChart:                   []models.VolumePoint{},
		RecentEvents:                  []models.RecentEvent{},
		IdentitiesUsingThisCredential: []models.UsingIdentity{},
		ErrorEvents:                   []models.IdentityErrorEvent{},
		Analytics: models.ActivityAnalytics{
			SourceIPs: []models.IPPct{}, ActiveRegions: []models.RegionCnt{},
			TopServices: []models.ServicePct{}, TopActions: []models.IdentityActionCount{},
			PeakHours: []models.IdentityHourCount{}, UsagePattern: "Dormant",
		},
		EventsPagination: models.EventsPagination{Page: p.page, PageSize: p.size},
	}
}

func identityActivityFrom(f *activityFetch, tr *ResolvedTimeRange, p activityPaging, enr *eventEnricher) *models.IdentityActivityResponse {
	resp := emptyIdentityActivity(tr, p)
	resp.VolumeChart = volumePoints(f.volume)
	for _, e := range f.events {
		resp.RecentEvents = append(resp.RecentEvents, models.IdentityRecentEvent{
			EventPrincipal: enr.principal(e),
			EventTime:      timePtr(e.EventTime), EventName: e.EventName, Service: chrepo.ServiceName(e.EventSource),
			ResourceARN: strPtr(e.ResourceARN), SourceIP: strPtr(e.SourceIP), Region: strPtr(e.Region),
			UserAgent: strPtr(e.UserAgent), ErrorCode: strPtr(e.ErrorCode), RequestID: strPtr(e.EventID),
		})
	}
	for _, e := range f.errors {
		resp.ErrorEvents = append(resp.ErrorEvents, errorEventFrom(e, enr))
	}
	a := ProcessActivityAnalytics(f.rows)
	resp.Analytics = models.IdentityActivityAnalytics{
		SourceIPs:           ipPcts(a.SourceIPs),
		ActiveRegions:       regionCounts(a.ActiveRegions),
		TopServices:         servicePcts(a.TopServices),
		TopActions:          actionCounts(a.TopActions),
		ErrorRatePct:        a.ErrorRatePct,
		AccessDeniedRatePct: a.AccessDeniedRatePct,
		PeakHours:           hourCounts(a.PeakHours),
		UsagePattern:        usagePatternFor(f.volume, tr),
	}
	resp.EventsPagination = pagination(f.total, p)
	return resp
}

func credentialActivityFrom(f *activityFetch, tr *ResolvedTimeRange, p activityPaging, enr *eventEnricher) *models.ActivityResponse {
	resp := emptyCredentialActivity(tr, p)
	resp.VolumeChart = volumePoints(f.volume)
	for _, e := range f.events {
		resp.RecentEvents = append(resp.RecentEvents, models.RecentEvent{
			EventPrincipal: enr.principal(e),
			EventTime:      timePtr(e.EventTime), EventName: e.EventName, Service: chrepo.ServiceName(e.EventSource),
			ResourceARN: strPtr(e.ResourceARN), SourceIP: strPtr(e.SourceIP), Region: strPtr(e.Region),
			UserAgent: strPtr(e.UserAgent), ErrorCode: strPtr(e.ErrorCode), EventCount: 1, RequestID: strPtr(e.EventID),
		})
	}
	for _, e := range f.errors {
		resp.ErrorEvents = append(resp.ErrorEvents, errorEventFrom(e, enr))
	}
	a := ProcessActivityAnalytics(f.rows)
	resp.Analytics = models.ActivityAnalytics{
		SourceIPs:           ipPcts(a.SourceIPs),
		ActiveRegions:       regionCounts(a.ActiveRegions),
		TopServices:         servicePcts(a.TopServices),
		UsagePattern:        usagePatternFor(f.volume, tr),
		ErrorRatePct:        a.ErrorRatePct,
		TopActions:          actionCounts(a.TopActions),
		AccessDeniedRatePct: a.AccessDeniedRatePct,
		PeakHours:           hourCounts(a.PeakHours),
	}
	resp.EventsPagination = pagination(f.total, p)
	return resp
}

func errorEventFrom(e chrepo.Event, enr *eventEnricher) models.IdentityErrorEvent {
	return models.IdentityErrorEvent{
		EventPrincipal: enr.principal(e),
		EventTime:      timePtr(e.EventTime), EventName: e.EventName, Service: chrepo.ServiceName(e.EventSource),
		ResourceARN: strPtr(e.ResourceARN), SourceIP: strPtr(e.SourceIP),
		ErrorCode: strPtr(e.ErrorCode), ErrorMessage: strPtr(e.ErrorMessage),
	}
}

func volumePoints(in []chrepo.VolumePoint) []models.VolumePoint {
	out := make([]models.VolumePoint, 0, len(in))
	for _, p := range in {
		out = append(out, models.VolumePoint{Date: p.Date, Count: int(p.Count)})
	}
	return out
}

func ipPcts(in []PctItem) []models.IPPct {
	out := []models.IPPct{}
	for i, it := range in {
		if i >= activityTopN {
			break
		}
		out = append(out, models.IPPct{IP: it.Key, Pct: it.Pct})
	}
	return out
}

func servicePcts(in []PctItem) []models.ServicePct {
	out := make([]models.ServicePct, 0, len(in))
	for _, it := range in {
		out = append(out, models.ServicePct{Service: it.Key, Pct: it.Pct, Count: int(it.Count)})
	}
	return out
}

func regionCounts(in []CountItem) []models.RegionCnt {
	out := make([]models.RegionCnt, 0, len(in))
	for _, it := range in {
		out = append(out, models.RegionCnt{Region: it.Key, Count: int(it.Count)})
	}
	return out
}

func actionCounts(in []CountItem) []models.IdentityActionCount {
	out := []models.IdentityActionCount{}
	for i, it := range in {
		if i >= activityTopN {
			break
		}
		out = append(out, models.IdentityActionCount{Action: it.Key, Count: int(it.Count)})
	}
	return out
}

func hourCounts(in []HourItem) []models.IdentityHourCount {
	out := []models.IdentityHourCount{}
	for i, it := range in {
		if i >= activityPeakHours {
			break
		}
		out = append(out, models.IdentityHourCount{Hour: it.Hour, Count: int(it.Count)})
	}
	return out
}

func pagination(total uint64, p activityPaging) models.EventsPagination {
	t := int(total)
	return models.EventsPagination{
		Page: p.page, PageSize: p.size, TotalCount: t,
		TotalPages: (t + p.size - 1) / p.size,
	}
}

// usagePatternFor labels how evenly activity is spread over the window. For
// "all" the window starts at the first active day rather than a century ago.
func usagePatternFor(volume []chrepo.VolumePoint, tr *ResolvedTimeRange) string {
	rangeDays := tr.Days
	if tr.Label == "all" && len(volume) > 0 {
		if first, err := time.Parse("2006-01-02", volume[0].Date); err == nil {
			rangeDays = int(tr.End.Sub(first).Hours()/24) + 1
		}
	}
	return ActivityUsagePattern(len(volume), rangeDays)
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	v := t
	return &v
}

// arnLastSegment is the display name for a caller that matched no identity.
func arnLastSegment(arn string) string {
	for i := len(arn) - 1; i >= 0; i-- {
		if arn[i] == '/' || arn[i] == ':' {
			if i == len(arn)-1 {
				return arn
			}
			return arn[i+1:]
		}
	}
	return arn
}
