package routes

import (
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/handlers/genome"
)

// identity_name is an ARN matched by {identity_name:.+}, so the sub-resource
// routes must win over the plain identity route.
func TestGenomeRoutesMatchSubResourcesFirst(t *testing.T) {
	r := mux.NewRouter()
	registerGenome(r, &GenomeHandlers{
		DashboardSummary: &genome.DashboardSummaryHandler{},
		IdentityActivity: &genome.IdentityActivityHandler{},
		IdentityGenome:   &genome.IdentityGenomeHandler{},
		IdentityTimeline: &genome.IdentityTimelineHandler{},
		IdentitySummary:  &genome.IdentitySummaryHandler{},
	})
	arn := "arn:aws:ecs:ap-south-2:884264984984:task%2Fcluster%2Fabc"
	for path, want := range map[string]string{
		"/genome/identities/ECS/" + arn + "/activity": "/genome/identities/{source_type}/{identity_name:.+}/activity",
		"/genome/identities/ECS/" + arn + "/genome":   "/genome/identities/{source_type}/{identity_name:.+}/genome",
		"/genome/identities/ECS/" + arn + "/timeline": "/genome/identities/{source_type}/{identity_name:.+}/timeline",
		"/genome/identities/ECS/" + arn:               "/genome/identities/{source_type}/{identity_name:.+}",
		"/genome/dashboard/summary":                   "/genome/dashboard/summary",
	} {
		var m mux.RouteMatch
		if !r.Match(httptest.NewRequest("GET", path, nil), &m) {
			t.Errorf("%s: no route", path)
			continue
		}
		if got, _ := m.Route.GetPathTemplate(); got != want {
			t.Errorf("%s matched %s, want %s", path, got, want)
		}
	}
}
