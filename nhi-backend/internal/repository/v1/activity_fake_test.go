package v1

import (
	"context"
	"time"

	chrepo "nhi-backend-api/internal/repository/clickhouse"
)

// fakeActivity is an in-memory ActivitySource for unit tests.
type fakeActivity struct {
	available bool
	degraded  bool
	seen      map[string]time.Time
	creators  map[string]string
	used      map[string][]chrepo.UsedAction
	perms     []chrepo.UsedPermission
	edges     []chrepo.AccessEdge

	seenCalls int
	lastSince time.Time // the "since" the most recent windowed call used
}

func (f *fakeActivity) Available() bool { return f.available }
func (f *fakeActivity) Degraded() bool  { return f.degraded }
func (f *fakeActivity) LastSeen(_ context.Context, keys []string) map[string]time.Time {
	f.seenCalls++
	out := map[string]time.Time{}
	for _, k := range keys {
		if t, ok := f.seen[k]; ok {
			out[k] = t
		}
	}
	return out
}
func (f *fakeActivity) Creators(_ context.Context, arns []string) map[string]string {
	out := map[string]string{}
	for _, a := range arns {
		if c, ok := f.creators[a]; ok {
			out[a] = c
		}
	}
	return out
}
func (f *fakeActivity) UsedActionSets(_ context.Context, keys []string, since time.Time) map[string][]chrepo.UsedAction {
	f.lastSince = since
	out := map[string][]chrepo.UsedAction{}
	for _, k := range keys {
		if u, ok := f.used[k]; ok {
			out[k] = u
		}
	}
	return out
}
func (f *fakeActivity) UsedPermissions(_ context.Context, _ []string, since time.Time) []chrepo.UsedPermission {
	f.lastSince = since
	return f.perms
}
func (f *fakeActivity) AccessEdges(_ context.Context, _ []string, since, _ time.Time) []chrepo.AccessEdge {
	f.lastSince = since
	return f.edges
}

func (f *fakeActivity) AccountVolume(context.Context, []string, time.Time) ([]chrepo.VolumePoint, bool) {
	return nil, false
}

var _ ActivitySource = (*fakeActivity)(nil)
