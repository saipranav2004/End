package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/joho/godotenv"

	"nhi-backend-api/internal/awsconnect"
	"nhi-backend-api/internal/clickhouse"
	"nhi-backend-api/internal/config"
	"nhi-backend-api/internal/githubcheck"
	"nhi-backend-api/internal/handlers"
	"nhi-backend-api/internal/handlers/credential_exposure"
	handlergenome "nhi-backend-api/internal/handlers/genome"
	posturehandlers "nhi-backend-api/internal/handlers/postures"
	handlerv1 "nhi-backend-api/internal/handlers/v1"
	"nhi-backend-api/internal/middleware"
	"nhi-backend-api/internal/repository"
	chrepo "nhi-backend-api/internal/repository/clickhouse"
	repoce "nhi-backend-api/internal/repository/credential_exposure"
	repogenome "nhi-backend-api/internal/repository/genome"
	repopostures "nhi-backend-api/internal/repository/postures"
	repov1 "nhi-backend-api/internal/repository/v1"
	"nhi-backend-api/internal/routes"
	"nhi-backend-api/internal/scope"
	"nhi-backend-api/internal/services"
	svcce "nhi-backend-api/internal/services/credential_exposure"
	svcgenome "nhi-backend-api/internal/services/genome"
	svcpostures "nhi-backend-api/internal/services/postures"
	svcv1 "nhi-backend-api/internal/services/v1"
)

func main() {
	// Optional .env for local runs; variables already in the environment win.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatalf("load .env: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}

	// DB is the global authentication/tenant registry pool. Tenant pools are
	// opened only through the registry-validated db_name in a signed token.
	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		log.Fatalf("connect to global database: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close database pools: %v", err)
		}
	}()

	// ClickHouse (CloudTrail events) is optional. Unconfigured: chClient is nil.
	// Unreachable: the client is kept and retried; until it answers the API
	// serves Postgres data only.
	chCtx, chCancel := context.WithTimeout(context.Background(), 10*time.Second)
	chClient, err := clickhouse.New(chCtx, cfg.ClickHouse)
	chCancel()
	switch {
	case err != nil:
		log.Printf("WARNING: clickhouse unavailable, activity data disabled until it answers: %v", err)
	case chClient == nil:
		log.Printf("clickhouse not configured; activity data disabled")
	default:
		log.Printf("clickhouse connected (%s:%d/%s)", cfg.ClickHouse.Host, cfg.ClickHouse.Port, cfg.ClickHouse.Database)
	}
	defer func() {
		if err := chClient.Close(); err != nil {
			log.Printf("close clickhouse: %v", err)
		}
	}()
	chRepo := chrepo.New(chClient)
	activityService := svcv1.NewActivityService(chRepo, repov1.NewIdentityLookupRepository())

	// Monitoring ClickHouse (NHI Genome log_events, NHI_MONITORING_CLICKHOUSE_*) is
	// optional too. If it is unconfigured or unreachable the genome endpoints
	// still serve Postgres data; the event-backed fields are then null.
	monitoringCfg, err := config.MonitoringClickHouse()
	if err != nil {
		log.Fatalf("load monitoring clickhouse configuration: %v", err)
	}
	monCtx, monCancel := context.WithTimeout(context.Background(), 10*time.Second)
	monitoringClient, err := clickhouse.New(monCtx, monitoringCfg)
	monCancel()
	switch {
	case err != nil:
		log.Printf("WARNING: monitoring clickhouse unavailable, event-backed fields disabled until it answers: %v", err)
	case monitoringClient == nil:
		log.Printf("monitoring clickhouse not configured (NHI_MONITORING_CLICKHOUSE_HOST empty); event-backed fields disabled")
	default:
		log.Printf("monitoring clickhouse connected (%s:%d/%s.%s)", monitoringCfg.Host, monitoringCfg.Port, monitoringCfg.Database, monitoringCfg.Table)
	}
	defer func() {
		if err := monitoringClient.Close(); err != nil {
			log.Printf("close monitoring clickhouse: %v", err)
		}
	}()
	monitoringEvents := repogenome.NewMonitoringEvents(monitoringClient, monitoringCfg.Database)

	userRepo := repository.NewUserRepository(db)
	resolver := scope.NewResolver(db)

	authService := services.NewAuthService(userRepo, cfg.JWT)
	authHandler := handlers.NewAuthHandler(authService)
	authMiddleware := middleware.NewAuthMiddleware(cfg.JWT, userRepo)

	filterHandler := handlerv1.NewFilterHandler(svcv1.NewFilterService(repov1.NewFilterRepository(), resolver))
	settingsHandler := handlerv1.NewSettingsHandler(svcv1.NewSettingsService(repov1.NewSettingsRepository(), db, resolver))
	// Scan change tracking (the scanner's nhi_scans_aws + scan_changes).
	scanChangesRepo := repov1.NewScanChangesRepository()
	scanChangesHandler := handlerv1.NewScanChangesHandler(svcv1.NewScanChangesService(scanChangesRepo, resolver))
	dashboardHandler := handlerv1.NewDashboardOverviewHandler(svcv1.NewDashboardOverviewService(repov1.NewDashboardOverviewRepository(), scanChangesRepo, resolver, activityService))
	identitiesHandler := handlerv1.NewIdentitiesListHandler(svcv1.NewIdentitiesListService(repov1.NewIdentitiesListRepository(), scanChangesRepo, resolver, activityService))
	identityDetailHandler := handlerv1.NewIdentityDetailHandler(svcv1.NewIdentityDetailService(repov1.NewIdentityDetailRepository(), scanChangesRepo, resolver, activityService))
	identityEventsHandler := handlerv1.NewIdentityEventsHandler(svcv1.NewIdentityEventsService(repov1.NewIdentityEventsRepository(), resolver, activityService))
	credentialsHandler := handlerv1.NewCredentialsHandler(svcv1.NewCredentialsService(repov1.NewCredentialsRepository(), scanChangesRepo, resolver, activityService))
	credentialDetailHandler := handlerv1.NewCredentialDetailHandler(svcv1.NewCredentialDetailService(repov1.NewCredentialDetailRepository(), scanChangesRepo, resolver, activityService))

	permissionSourceHandler := handlerv1.NewPermissionSourceHandler(svcv1.NewPermissionSourceService(repov1.NewPermissionSourceRepository(), resolver))

	// ISPM posture dashboard API — 16 endpoints under /api/v1/ispm, tenant from the JWT only.
	postureHandlers := &posturehandlers.Handlers{
		Overview:            posturehandlers.NewPostureOverviewHandler(svcpostures.NewPostureOverviewService(repopostures.NewPostureOverviewRepository(), resolver)),
		QuickWinIdentities:  posturehandlers.NewQuickWinIdentitiesHandler(svcpostures.NewQuickWinIdentitiesService(repopostures.NewQuickWinIdentitiesRepository(), resolver)),
		Export:              posturehandlers.NewPostureExportHandler(svcpostures.NewPostureExportService(repopostures.NewPostureExportRepository(), resolver)),
		IdentitiesList:      posturehandlers.NewIdentitiesListHandler(svcpostures.NewIdentitiesListService(repopostures.NewIdentitiesListRepository(), resolver)),
		IdentitiesFacets:    posturehandlers.NewIdentitiesFacetsHandler(svcpostures.NewIdentitiesFacetsService(repopostures.NewIdentitiesFacetsRepository(), resolver)),
		IdentityHeader:      posturehandlers.NewIdentityHeaderHandler(svcpostures.NewIdentityHeaderService(repopostures.NewIdentityHeaderRepository(), resolver)),
		IdentityPosture:     posturehandlers.NewIdentityPostureHandler(svcpostures.NewIdentityPostureService(repopostures.NewIdentityPostureRepository(), resolver)),
		IdentityChecks:      posturehandlers.NewIdentityChecksHandler(svcpostures.NewIdentityChecksService(repopostures.NewIdentityChecksRepository(), resolver)),
		IdentityHistory:     posturehandlers.NewIdentityHistoryHandler(svcpostures.NewIdentityHistoryService(repopostures.NewIdentityHistoryRepository(), resolver)),
		IdentityCredentials: posturehandlers.NewIdentityCredentialsHandler(svcpostures.NewIdentityCredentialsService(repopostures.NewIdentityCredentialsRepository(), resolver)),
		IdentityRoleUse:     posturehandlers.NewIdentityRoleUseHandler(svcpostures.NewIdentityRoleUseService(repopostures.NewIdentityRoleUseRepository(), resolver)),
		IdentityActivity: posturehandlers.NewIdentityActivityHandler(svcpostures.NewIdentityActivityService(
			repopostures.NewIdentityActivityRepository(chRepo, repov1.NewIdentityLookupRepository()), resolver)),
		IdentityAlerts: posturehandlers.NewIdentityAlertsHandler(svcpostures.NewIdentityAlertsService(repopostures.NewIdentityAlertsRepository(), resolver)),
		IdentityGraph:  posturehandlers.NewIdentityGraphHandler(svcpostures.NewIdentityGraphService(repopostures.NewIdentityGraphRepository(), resolver)),
		CheckDetail:    posturehandlers.NewCheckDetailHandler(svcpostures.NewCheckDetailService(repopostures.NewCheckDetailRepository(), resolver)),
		FindingUpdate:  posturehandlers.NewFindingUpdateHandler(svcpostures.NewFindingUpdateService(repopostures.NewFindingUpdateRepository(), resolver)),
	}

	// NHI Genome (backend_api_contract.md §4.1, §5) under /api/v1/genome — Postgres
	// through the tenant scope, ClickHouse log_events through monitoringEvents.
	genomeIdentities := svcgenome.NewMonitoredIdentityLookup(repogenome.NewDashboardSummaryRepository(), resolver)
	genomeHandlers := &routes.GenomeHandlers{
		DashboardSummary: handlergenome.NewDashboardSummaryHandler(svcgenome.NewDashboardSummaryService(repogenome.NewDashboardSummaryRepository(), resolver)),
		IdentityActivity: handlergenome.NewIdentityActivityHandler(svcgenome.NewIdentityActivityService(genomeIdentities, monitoringEvents)),
		IdentityGenome:   handlergenome.NewIdentityGenomeHandler(svcgenome.NewIdentityGenomeService(genomeIdentities, monitoringEvents)),
		IdentityTimeline: handlergenome.NewIdentityTimelineHandler(svcgenome.NewIdentityTimelineService(genomeIdentities, repogenome.NewIdentityTimelineRepository())),
		IdentitySummary:  handlergenome.NewIdentitySummaryHandler(svcgenome.NewIdentitySummaryService(genomeIdentities, repogenome.NewIdentitySummaryRepository(), monitoringEvents)),
	}

	// Connect AWS Account. The SDK's default chain supplies this process's
	// identity (task role, instance profile, AWS_PROFILE); credentials resolve
	// on first use, so a host without AWS access still serves every other route.
	var awsVerifier svcv1.AWSVerifier
	var awsClient *awsconnect.Client
	awsCtx, awsCancel := context.WithTimeout(context.Background(), 10*time.Second)
	awsBase, err := awsconfig.LoadDefaultConfig(awsCtx)
	awsCancel()
	if err != nil {
		log.Printf("aws integration: AWS configuration unavailable, connection checks disabled: %v", err)
	} else {
		awsClient = awsconnect.NewClient(awsBase, cfg.AWSIntegration.AssumeAssumerRole, cfg.AWSIntegration.EventLogGroup)
		awsVerifier = awsClient
	}
	// "Events received" reads the event store the pipeline fills from the
	// central bus: ClickHouse, else the bus's CloudWatch log group.
	var awsEvents svcv1.AWSEventSource
	switch {
	case chClient != nil:
		awsEvents = svcv1.ClickHouseEventSource{Repo: chrepo.New(chClient)}
	case awsClient != nil && cfg.AWSIntegration.EventLogGroup != "":
		awsEvents = awsClient
	default:
		log.Printf("aws integration: no event store (ClickHouse or aws_integration.event_log_group); the events check cannot run")
	}
	awsIntegrationHandler := handlerv1.NewAWSIntegrationHandler(svcv1.NewAWSIntegrationService(
		repository.NewAWSIntegrationRepository(db),
		awsVerifier,
		githubcheck.New(cfg.AWSIntegration.GitHubAPIURL),
		awsEvents,
		svcv1.AWSIntegrationOptions{
			AssumerRoleName:    cfg.AWSIntegration.AssumerRoleName,
			CollectorRoleName:  cfg.AWSIntegration.CollectorRoleName,
			ForwardRuleName:    cfg.AWSIntegration.ForwardRuleName,
			GitHubSecretPrefix: cfg.AWSIntegration.GitHubSecretPrefix,
			EventLookback:      time.Duration(cfg.AWSIntegration.EventLookbackHours) * time.Hour,
		},
	))

	// Credential Exposure — tenant-isolated, admin-only
	ceFindingsRepo := repoce.NewFindingsRepository()
	ceAllowlistRepo := repoce.NewAllowlistRepository()
	ceDecisionRepo := repoce.NewDecisionRepository()
	ceDeepScanRepo := repoce.NewDeepScanRepository()

	ceFindingsService := svcce.NewFindingsService(ceFindingsRepo, resolver)
	ceAllowlistService := svcce.NewAllowlistService(ceAllowlistRepo, resolver)
	ceDecisionService := svcce.NewDecisionService(ceDecisionRepo, resolver)
	ceDeepScanService := svcce.NewDeepScanService(ceDeepScanRepo, resolver)

	ceFindingsHandler := credential_exposure.NewFindingsHandler(ceFindingsService)
	ceAllowlistHandler := credential_exposure.NewAllowlistHandler(ceAllowlistService)
	ceDecisionHandler := credential_exposure.NewDecisionHandler(ceDecisionService)
	ceDeepScanHandler := credential_exposure.NewDeepScanHandler(ceDeepScanService)

	router := routes.SetupRoutes(routes.HandlerContainer{
		AuthHandler:                    authHandler,
		AuthMiddleware:                 authMiddleware,
		NewAPIFilterHandler:            filterHandler,
		NewAPISettingsHandler:          settingsHandler,
		NewAPIDashboardOverviewHandler: dashboardHandler,
		ScanChangesHandler:             scanChangesHandler,
		NewAPIIdentitiesListHandler:    identitiesHandler,
		NewAPIIdentityDetailHandler:    identityDetailHandler,
		NewAPIIdentityEventsHandler:    identityEventsHandler,
		NewAPICredentialsHandler:       credentialsHandler,
		NewAPICredentialDetailHandler:  credentialDetailHandler,
		AWSIntegrationHandler:          awsIntegrationHandler,
		NewAPIPermissionSourceHandler:  permissionSourceHandler,
		PostureHandlers:                postureHandlers,
		GenomeHandlers:                 genomeHandlers,

		CredentialExposureFindingsHandler:  ceFindingsHandler,
		CredentialExposureAllowlistHandler: ceAllowlistHandler,
		CredentialExposureDecisionHandler:  ceDecisionHandler,
		CredentialExposureDeepScanHandler:  ceDeepScanHandler,
	})

	// Readiness: 200 when Postgres answers (ClickHouse is optional and only
	// reported), 503 otherwise. /health stays a plain liveness probe.
	router.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		state := func(c *clickhouse.Client) string {
			switch {
			case c == nil:
				return "not_configured"
			case c.Up():
				return "ok"
			}
			return "down"
		}
		body := map[string]string{"postgres": "ok", "clickhouse": state(chClient), "monitoring_clickhouse": state(monitoringClient)}
		status := http.StatusOK
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := db.DB.PingContext(ctx); err != nil {
			log.Printf("readiness: postgres: %v", err)
			body["postgres"], status = "down", http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}).Methods(http.MethodGet)

	server := &http.Server{
		Addr:        fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:     middleware.SetupCORS(cfg.CORS)(router),
		ReadTimeout: 15 * time.Second,
		// Above the slowest handler: AWS verify runs up to 12 s of checks and
		// then a 5 s save; queries stop at the 25 s statement_timeout.
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	// Drain in-flight requests on SIGINT/SIGTERM so deferred cleanup (pool
	// close) runs instead of the process being killed mid-request.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	log.Printf("NHI API server listening on %s", server.Addr)

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			// Keep the original non-zero exit; close pools first since Fatalf skips defers.
			_ = db.Close()
			log.Fatalf("API server: %v", err)
		}
	case <-sigCtx.Done():
		log.Printf("shutdown signal received, draining requests")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown: %v", err)
		}
	}
}
