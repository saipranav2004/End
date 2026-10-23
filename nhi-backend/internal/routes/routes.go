package routes

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"nhi-backend-api/internal/handlers"
	"nhi-backend-api/internal/handlers/credential_exposure"
	"nhi-backend-api/internal/handlers/genome"
	posturehandlers "nhi-backend-api/internal/handlers/postures"
	handlerv1 "nhi-backend-api/internal/handlers/v1"
	"nhi-backend-api/internal/middleware"
)

type HandlerContainer struct {
	AuthHandler    *handlers.AuthHandler
	AuthMiddleware *middleware.AuthMiddleware

	NewAPIFilterHandler            *handlerv1.FilterHandler
	NewAPISettingsHandler          *handlerv1.SettingsHandler
	NewAPIDashboardOverviewHandler *handlerv1.DashboardOverviewHandler
	DashboardTrendsHandler         *handlerv1.DashboardTrendsHandler
	ScanChangesHandler             *handlerv1.ScanChangesHandler
	NewAPIIdentitiesListHandler    *handlerv1.IdentitiesListHandler
	NewAPIIdentityDetailHandler    *handlerv1.IdentityDetailHandler
	NewAPIIdentityEventsHandler    *handlerv1.IdentityEventsHandler
	NewAPICredentialsHandler       *handlerv1.CredentialsHandler
	NewAPICredentialDetailHandler  *handlerv1.CredentialDetailHandler
	NewAPIPermissionSourceHandler  *handlerv1.PermissionSourceHandler
	AWSIntegrationHandler          *handlerv1.AWSIntegrationHandler

	// ISPM posture dashboard API (16 endpoints), mounted under PostureBasePath.
	PostureHandlers *posturehandlers.Handlers

	// NHI Genome (backend_api_contract.md §4.1 and §5), mounted under GenomeBasePath.
	GenomeHandlers *GenomeHandlers

	CredentialExposureFindingsHandler  *credential_exposure.FindingsHandler
	CredentialExposureAllowlistHandler *credential_exposure.AllowlistHandler
	CredentialExposureDecisionHandler  *credential_exposure.DecisionHandler
	CredentialExposureDeepScanHandler  *credential_exposure.DeepScanHandler
}

func SetupRoutes(h HandlerContainer) *mux.Router {
	r := mux.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger)
	r.Use(middleware.Recoverer)

	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"success": false,
			"message": "route not found: " + req.Method + " " + req.URL.Path,
			"error":   "ROUTE_NOT_FOUND",
		})
	})
	r.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"success": false,
			"message": "method not allowed: " + req.Method + " " + req.URL.Path,
		})
	})

	r.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "healthy", "service": "nhi-backend-api"})
	}).Methods(http.MethodGet)

	if h.AuthHandler != nil {
		r.HandleFunc("/api/v1/login", h.AuthHandler.Login).Methods(http.MethodPost)
		r.HandleFunc("/api/auth/login", h.AuthHandler.Login).Methods(http.MethodPost)
		r.HandleFunc("/api/v1/refresh", h.AuthHandler.Refresh).Methods(http.MethodPost)
		r.HandleFunc("/api/auth/refresh", h.AuthHandler.Refresh).Methods(http.MethodPost)
		r.HandleFunc("/api/v1/logout", h.AuthHandler.Logout).Methods(http.MethodPost)
		r.HandleFunc("/api/auth/logout", h.AuthHandler.Logout).Methods(http.MethodPost)
	}
	if h.AuthMiddleware == nil {
		return r
	}

	v1 := r.PathPrefix("/api/v1").Subrouter()
	v1.Use(h.AuthMiddleware.Authenticate)
	protected := r.PathPrefix("/api").Subrouter()
	protected.Use(h.AuthMiddleware.Authenticate)
	registerProtected(v1, h, true)
	registerProtected(protected, h, false)
	return r
}

func registerProtected(router *mux.Router, h HandlerContainer, versioned bool) {
	if h.AuthHandler != nil {
		path := "/auth/me"
		if versioned {
			path = "/me"
		}
		router.HandleFunc(path, h.AuthHandler.Me).Methods(http.MethodGet)
	}
	if h.NewAPIFilterHandler != nil {
		router.HandleFunc("/filters", h.NewAPIFilterHandler.GetFilters).Methods(http.MethodGet)
	}
	if h.NewAPISettingsHandler != nil {
		router.HandleFunc("/settings", h.NewAPISettingsHandler.Get).Methods(http.MethodGet)
		router.HandleFunc("/settings", h.NewAPISettingsHandler.Update).Methods(http.MethodPut)
	}
	if h.NewAPIDashboardOverviewHandler != nil {
		router.HandleFunc("/dashboard/overview", h.NewAPIDashboardOverviewHandler.Get).Methods(http.MethodGet)
	}
	if h.DashboardTrendsHandler != nil {
		router.HandleFunc("/dashboard/trends", h.DashboardTrendsHandler.Get).Methods(http.MethodGet)
	}
	// Scan change tracking: scans and what each changed.
	if h.ScanChangesHandler != nil {
		router.HandleFunc("/scans", h.ScanChangesHandler.List).Methods(http.MethodGet)
		router.HandleFunc("/scans/{scan_id}/changes", h.ScanChangesHandler.Changes).Methods(http.MethodGet)
	}
	if h.NewAPIIdentitiesListHandler != nil {
		router.HandleFunc("/identities", h.NewAPIIdentitiesListHandler.List).Methods(http.MethodGet)
	}
	if h.NewAPIIdentityDetailHandler != nil {
		router.HandleFunc("/identities/{id}", h.NewAPIIdentityDetailHandler.Get).Methods(http.MethodGet)
	}
	if h.NewAPIIdentityEventsHandler != nil {
		router.HandleFunc("/identities/{id}/events", h.NewAPIIdentityEventsHandler.Get).Methods(http.MethodGet)
	}
	if h.NewAPICredentialsHandler != nil {
		router.HandleFunc("/credentials", h.NewAPICredentialsHandler.List).Methods(http.MethodGet)
	}
	if h.NewAPICredentialDetailHandler != nil {
		router.HandleFunc("/credentials/{id}", h.NewAPICredentialDetailHandler.Get).Methods(http.MethodGet)
	}
	if h.NewAPIPermissionSourceHandler != nil {
		router.HandleFunc("/permission-source", h.NewAPIPermissionSourceHandler.Get).Methods(http.MethodGet)
	}
	// Connect AWS Account (Integrations page). Writes are admin-only.
	if h.AWSIntegrationHandler != nil {
		router.HandleFunc("/integrations/aws", h.AWSIntegrationHandler.Get).Methods(http.MethodGet)
		router.HandleFunc("/integrations/aws/verify", h.AWSIntegrationHandler.Verify).Methods(http.MethodPost)
		router.HandleFunc("/integrations/aws/accounts/{account_id}/regions", h.AWSIntegrationHandler.UpdateRegions).Methods(http.MethodPut)
		router.HandleFunc("/integrations/aws/accounts/{account_id}/exposure", h.AWSIntegrationHandler.Exposure).Methods(http.MethodPut)
		router.HandleFunc("/integrations/aws/accounts/{account_id}/sync", h.AWSIntegrationHandler.Sync).Methods(http.MethodPost)
		router.HandleFunc("/integrations/aws/accounts/{account_id}", h.AWSIntegrationHandler.Remove).Methods(http.MethodDelete)
	}
	registerPostures(router, h.PostureHandlers)
	registerGenome(router, h.GenomeHandlers)
	// Credential Exposure — secret scanner findings, tenant-isolated, admin-only
	// These were present in the original backends but not wired in the v1-only build.
	// They live in their own package with strict query validation and tenant scope.
	if h.CredentialExposureFindingsHandler != nil {
		router.HandleFunc("/credential-exposure/findings", h.CredentialExposureFindingsHandler.List).Methods(http.MethodGet)
		router.HandleFunc("/credential-exposure/findings/flagged", h.CredentialExposureFindingsHandler.ListFlagged).Methods(http.MethodGet)
		router.HandleFunc("/credential-exposure/findings/ml-flagged", h.CredentialExposureFindingsHandler.ListFlagged).Methods(http.MethodGet)
		router.HandleFunc("/credential-exposure/findings/review", h.CredentialExposureFindingsHandler.ListReview).Methods(http.MethodGet)
		router.HandleFunc("/credential-exposure/findings/repeated", h.CredentialExposureFindingsHandler.ListRepeated).Methods(http.MethodGet)
		router.HandleFunc("/credential-exposure/findings/noise", h.CredentialExposureFindingsHandler.ListNoise).Methods(http.MethodGet)
		router.HandleFunc("/credential-exposure/findings/summary", h.CredentialExposureFindingsHandler.Summary).Methods(http.MethodGet)
	}
	if h.CredentialExposureAllowlistHandler != nil {
		router.HandleFunc("/credential-exposure/allowlist", h.CredentialExposureAllowlistHandler.List).Methods(http.MethodGet)
		router.HandleFunc("/credential-exposure/allowlist", h.CredentialExposureAllowlistHandler.Create).Methods(http.MethodPost)
		router.HandleFunc("/credential-exposure/allowlist", h.CredentialExposureAllowlistHandler.Delete).Methods(http.MethodDelete)
	}
	if h.CredentialExposureDecisionHandler != nil {
		router.HandleFunc("/credential-exposure/decisions", h.CredentialExposureDecisionHandler.Save).Methods(http.MethodPost)
	}
	if h.CredentialExposureDeepScanHandler != nil {
		router.HandleFunc("/credential-exposure/deep-scan", h.CredentialExposureDeepScanHandler.Trigger).Methods(http.MethodPost)
		router.HandleFunc("/credential-exposure/deep-scan/status", h.CredentialExposureDeepScanHandler.Status).Methods(http.MethodGet)
	}
}

// PostureBasePath is where the ISPM posture dashboard API is mounted, below
// /api/v1 (and /api). /identities and /identities/{id} already belong to the
// inventory API, so the posture API lives under its own prefix; change this
// one constant to move all 16 endpoints.
const PostureBasePath = "/ispm"

// registerPostures mounts the 16 endpoints of ISPM_Dashboard_API_v2.md.
// Static paths are registered before the {id} ones so that /identities/facets
// is not read as an identity id.
func registerPostures(router *mux.Router, p *posturehandlers.Handlers) {
	if p == nil {
		return
	}
	r := router.PathPrefix(PostureBasePath).Subrouter()
	get := func(path string, h http.HandlerFunc) { r.HandleFunc(path, h).Methods(http.MethodGet) }

	if p.Overview != nil {
		get("/posture/overview", p.Overview.Get)
	}
	if p.QuickWinIdentities != nil {
		get("/posture/quick-wins/{rule_id}/identities", p.QuickWinIdentities.Get)
	}
	if p.Export != nil {
		get("/posture/export", p.Export.Get)
	}
	if p.IdentitiesList != nil {
		get("/identities", p.IdentitiesList.List)
	}
	if p.IdentitiesFacets != nil {
		get("/identities/facets", p.IdentitiesFacets.Get)
	}
	if p.IdentityHeader != nil {
		get("/identities/{id}", p.IdentityHeader.Get)
	}
	if p.IdentityPosture != nil {
		get("/identities/{id}/posture", p.IdentityPosture.Get)
	}
	if p.IdentityChecks != nil {
		get("/identities/{id}/checks", p.IdentityChecks.Get)
	}
	if p.IdentityHistory != nil {
		get("/identities/{id}/history", p.IdentityHistory.Get)
	}
	if p.IdentityCredentials != nil {
		get("/identities/{id}/credentials", p.IdentityCredentials.Get)
	}
	if p.IdentityRoleUse != nil {
		get("/identities/{id}/role-use", p.IdentityRoleUse.Get)
	}
	if p.IdentityActivity != nil {
		get("/identities/{id}/activity", p.IdentityActivity.Get)
	}
	if p.IdentityAlerts != nil {
		get("/identities/{id}/alerts", p.IdentityAlerts.Get)
	}
	if p.IdentityGraph != nil {
		get("/identities/{id}/graph", p.IdentityGraph.Get)
	}
	if p.CheckDetail != nil {
		get("/checks/{rule_id}", p.CheckDetail.Get)
	}
	if p.FindingUpdate != nil {
		r.HandleFunc("/findings/{id}", p.FindingUpdate.Patch).Methods(http.MethodPatch)
	}
}

// GenomeBasePath is where the NHI Genome API is mounted, below /api/v1 (and
// /api). Its /identities list and /identities/{source_type}/{identity_name}
// pages would otherwise collide with the discovery API's /identities and
// /identities/{id}/events. Keep in step with genome service APIBasePath.
const GenomeBasePath = "/genome"

// GenomeHandlers are the NHI Genome endpoints: the fleet summary and, per
// identity, its behavioural genome, activity and timeline. Identities are
// listed from the discovery inventory, not here.
type GenomeHandlers struct {
	DashboardSummary *genome.DashboardSummaryHandler
	IdentityActivity *genome.IdentityActivityHandler
	IdentityGenome   *genome.IdentityGenomeHandler
	IdentityTimeline *genome.IdentityTimelineHandler
	IdentitySummary  *genome.IdentitySummaryHandler
}

// registerGenome mounts the genome endpoints. identity_name is an ARN (it
// contains ':' and '/'), so the patterns use {identity_name:.+}.
func registerGenome(router *mux.Router, g *GenomeHandlers) {
	if g == nil {
		return
	}
	r := router.PathPrefix(GenomeBasePath).Subrouter()
	get := func(path string, h http.HandlerFunc) { r.HandleFunc(path, h).Methods(http.MethodGet) }

	if g.DashboardSummary != nil {
		get("/dashboard/summary", g.DashboardSummary.Get)
	}
	if g.IdentityActivity != nil {
		get("/identities/{source_type}/{identity_name:.+}/activity", g.IdentityActivity.Get)
	}
	if g.IdentityGenome != nil {
		get("/identities/{source_type}/{identity_name:.+}/genome", g.IdentityGenome.Get)
	}
	if g.IdentityTimeline != nil {
		get("/identities/{source_type}/{identity_name:.+}/timeline", g.IdentityTimeline.Get)
	}
	// Last: identity_name is greedy, so the sub-resource routes must match first.
	if g.IdentitySummary != nil {
		get("/identities/{source_type}/{identity_name:.+}", g.IdentitySummary.Get)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
