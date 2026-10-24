package routes

import (
	"net/http"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/genomefull/anomalydetail"
	"nhi-backend-api/internal/genomefull/anomalydisposition"
	"nhi-backend-api/internal/genomefull/anomalyevidence"
	"nhi-backend-api/internal/genomefull/anomalyfeedback"
	"nhi-backend-api/internal/genomefull/anomalylist"
	"nhi-backend-api/internal/genomefull/common"
	"nhi-backend-api/internal/genomefull/containmentfreeze"
	"nhi-backend-api/internal/genomefull/containmentlist"
	"nhi-backend-api/internal/genomefull/dashboardactivity"
	"nhi-backend-api/internal/genomefull/dashboarddrift"
	"nhi-backend-api/internal/genomefull/identitybaseline"
	"nhi-backend-api/internal/genomefull/identitycontainment"
	"nhi-backend-api/internal/genomefull/identitydrift"
	"nhi-backend-api/internal/genomefull/identitylist"
	"nhi-backend-api/internal/genomefull/identitypeers"
	"nhi-backend-api/internal/scope"
)

// GenomeFull is the rest of the NHI Genome contract (backend_api_contract.md):
// fleet list, call-volume and drift dashboards, baseline, peers, drift,
// anomalies (list, detail, evidence, feedback, disposition) and containment.
// Containment and disposition answer 501 until their back ends exist, as the
// contract says. Each package answers in the contract's own envelope
// ({data, meta} / {error}) and honours NHI_GENOME_WORKLOAD_TYPES.
type GenomeFull struct {
	dashboardActivity   *dashboardactivity.Handler
	dashboardDrift      *dashboarddrift.Handler
	identityList        *identitylist.Handler
	identityBaseline    *identitybaseline.Handler
	identityPeers       *identitypeers.Handler
	identityDrift       *identitydrift.Handler
	identityContainment *identitycontainment.Handler
	containmentFreeze   *containmentfreeze.Handler
	containmentList     *containmentlist.Handler
	anomalyList         *anomalylist.Handler
	anomalyDetail       *anomalydetail.Handler
	anomalyEvidence     *anomalyevidence.Handler
	anomalyFeedback     *anomalyfeedback.Handler
	anomalyDisposition  *anomalydisposition.Handler
}

// NewGenomeFull wires the packages. events may be nil (no monitoring
// ClickHouse): event-backed fields are then null instead of failing.
func NewGenomeFull(resolver *scope.Resolver, events *common.MonitoringEvents) *GenomeFull {
	lookup := common.NewMonitoredIdentityLookup(resolver)
	driftRepo := identitydrift.NewRepository()
	return &GenomeFull{
		dashboardActivity:   dashboardactivity.NewHandler(dashboardactivity.NewService(lookup, dashboardactivity.NewRepository(), events)),
		dashboardDrift:      dashboarddrift.NewHandler(dashboarddrift.NewService(dashboarddrift.NewRepository(), resolver)),
		identityList:        identitylist.NewHandler(identitylist.NewService(lookup, identitylist.NewRepository(), events)),
		identityBaseline:    identitybaseline.NewHandler(identitybaseline.NewService(lookup, identitybaseline.NewRepository(), driftRepo)),
		identityPeers:       identitypeers.NewHandler(identitypeers.NewService(lookup, identitypeers.NewRepository(), events)),
		identityDrift:       identitydrift.NewHandler(identitydrift.NewService(lookup, driftRepo)),
		identityContainment: identitycontainment.NewHandler(identitycontainment.NewService(lookup)),
		containmentFreeze:   containmentfreeze.NewHandler(containmentfreeze.NewService(lookup)),
		containmentList:     containmentlist.NewHandler(containmentlist.NewService()),
		anomalyList:         anomalylist.NewHandler(anomalylist.NewService(lookup, anomalylist.NewRepository(), events)),
		anomalyDetail:       anomalydetail.NewHandler(anomalydetail.NewService(lookup, anomalydetail.NewRepository(), events)),
		anomalyEvidence:     anomalyevidence.NewHandler(anomalyevidence.NewService(lookup, anomalyevidence.NewRepository(), events)),
		anomalyFeedback:     anomalyfeedback.NewHandler(anomalyfeedback.NewService(anomalyfeedback.NewRepository(), resolver)),
		anomalyDisposition:  anomalydisposition.NewHandler(anomalydisposition.NewService()),
	}
}

// register mounts the routes on the /genome subrouter. The identity
// sub-resources go first: identity_name is greedy ({identity_name:.+}), so the
// plain identity route must be last.
func (g *GenomeFull) register(r *mux.Router) {
	if g == nil {
		return
	}
	get := func(path string, h http.HandlerFunc) { r.HandleFunc(path, h).Methods(http.MethodGet) }
	post := func(path string, h http.HandlerFunc) { r.HandleFunc(path, h).Methods(http.MethodPost) }

	get("/dashboard/activity", g.dashboardActivity.Get)
	get("/dashboard/drift", g.dashboardDrift.Get)
	get("/identities", g.identityList.Get)
	get("/identities/{source_type}/{identity_name:.+}/baseline", g.identityBaseline.Get)
	get("/identities/{source_type}/{identity_name:.+}/peers", g.identityPeers.Get)
	get("/identities/{source_type}/{identity_name:.+}/drift", g.identityDrift.Get)
	get("/identities/{source_type}/{identity_name:.+}/containment", g.identityContainment.Get)
	post("/identities/{source_type}/{identity_name:.+}/containment/freeze", g.containmentFreeze.Post)
	get("/anomalies", g.anomalyList.Get)
	get("/anomalies/{alert_id}", g.anomalyDetail.Get)
	get("/anomalies/{alert_id}/evidence", g.anomalyEvidence.Get)
	post("/anomalies/{alert_id}/feedback", g.anomalyFeedback.Post)
	post("/anomalies/{alert_id}/disposition", g.anomalyDisposition.Post)
	get("/containment", g.containmentList.Get)
}
