package server

import "net/http"

// The gateway's URL surface, kept apart from server construction so adding an
// endpoint and changing how the server is built stay separate edits.

func (s *Server) routes() {
	s.registerSingleMethodRoute(http.MethodGet, "/livez", s.handleLive, jsonMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/readyz", s.handleHealth, jsonMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/healthz", s.handleHealth, jsonMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/metrics", s.handleMetrics, s.metricsMethodNotAllowed(http.MethodGet))
	s.registerModelRoutes()
	// The in-flight gauge covers exactly the endpoints that route to an upstream, so
	// it stays comparable with requests_total. Catalog lookups and count_tokens are
	// local and never produce a request count, and admin traffic and scrapes are not
	// gateway load at all.
	s.registerSingleMethodRoute(http.MethodPost, "/v1/chat/completions", s.gatewayInFlight(s.handleChatCompletions), jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/v1/messages", s.gatewayInFlight(s.handleAnthropicMessages), anthropicMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/v1/messages/count_tokens", s.handleAnthropicCountTokens, anthropicMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/v1/responses", s.gatewayInFlight(s.handleResponses), jsonMethodNotAllowed(http.MethodPost))
	s.registerDynamicGETRoute("/v1/responses/{response_id}", s.handleResponseJobGet, responseJobGetHeadFallback)
	s.registerDynamicGETRoute("/v1/responses/{response_id}/{$}", s.handleResponseJobGet, responseJobGetHeadFallback)
	s.registerDynamicMethodRoute(http.MethodPost, "/v1/responses/{response_id}/cancel", s.handleResponseJobCancel)
	s.registerDynamicMethodRoute(http.MethodPost, "/v1/responses/{response_id}/cancel/{$}", s.handleResponseJobCancel)
	s.mux.HandleFunc(http.MethodPost+" /v1/responses/compact", s.gatewayInFlight(s.handleResponsesCompact))
	s.mux.HandleFunc(http.MethodGet+" /v1/responses/compact", jsonMethodNotAllowed(http.MethodPost))
	s.mux.HandleFunc("/v1/responses/", s.handleResponseJob)
	s.registerSingleMethodRoute(http.MethodPost, "/v1/embeddings", s.gatewayInFlight(s.handleEmbeddings), jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/v1/images/generations", s.handleImageGenerations, jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/v1/images/edits", s.handleImageEdits, jsonMethodNotAllowed(http.MethodPost))
	s.registerDynamicGETRoute("/v1/image-jobs/{job_id}", s.handleImageJobGet, jsonMethodNotAllowed(http.MethodGet))
	s.registerDynamicGETRoute("/v1/image-jobs/{job_id}/{$}", s.handleImageJobGet, jsonMethodNotAllowed(http.MethodGet))
	s.mux.HandleFunc("/v1/image-jobs/", s.handleImageJob)
	s.registerDynamicGETRoute("/v1/image-assets/{asset_id}/content", s.handleImageAssetGet, jsonMethodNotAllowed(http.MethodGet))
	s.registerDynamicGETRoute("/v1/image-assets/{asset_id}/content/{$}", s.handleImageAssetGet, jsonMethodNotAllowed(http.MethodGet))
	s.mux.HandleFunc("/v1/image-assets/", s.handleImageAsset)
	s.registerSingleMethodRoute(http.MethodGet, "/api/v1/analytics/token-costs", s.handleTokenCostAnalytics, jsonMethodNotAllowed(http.MethodGet))

	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/auth/login", s.handleAdminLogin, jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/auth/logout", s.handleAdminLogout, jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/auth/me", s.handleAdminMe, s.adminAuthenticationMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/auth/reset-password", s.handleAdminResetPassword, jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/auth/identity-providers", s.handleAdminAuthIdentityProviders, jsonMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/auth/oauth/start", s.handleAdminOAuthStart, jsonMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/auth/oauth/callback", s.handleAdminOAuthCallback, jsonMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/overview", s.handleAdminOverview, s.adminMethodNotAllowed("overview", http.MethodGet))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/playground/chat", s.handleAdminPlaygroundChat, s.adminMethodNotAllowed("playground", http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/playground/chat/stream", s.handleAdminPlaygroundChatStream, s.adminMethodNotAllowed("playground", http.MethodPost))
	s.registerMethodRoutes("/api/admin/projects", func(allowedMethods string) http.HandlerFunc {
		return s.adminMethodNotAllowed("project", allowedMethods)
	},
		methodRoute{Method: http.MethodGet, Handler: s.handleAdminProjectsGet},
		methodRoute{Method: http.MethodPost, Handler: s.handleAdminProjectsPost},
	)
	s.registerMethodRoutes("/api/admin/projects/{project_id}", func(allowedMethods string) http.HandlerFunc {
		return s.adminProjectMethodNotAllowed("project", allowedMethods)
	},
		methodRoute{Method: http.MethodPatch, Handler: s.handleAdminProjectPatch},
		methodRoute{Method: http.MethodDelete, Handler: s.handleAdminProjectDelete},
	)
	s.registerMethodRoutes("/api/admin/projects/{project_id}/keys", func(allowedMethods string) http.HandlerFunc {
		return s.adminProjectMethodNotAllowed("api_key", allowedMethods)
	},
		methodRoute{Method: http.MethodGet, Handler: s.handleAdminProjectKeysGet},
		methodRoute{Method: http.MethodPost, Handler: s.handleAdminProjectKeysPost},
	)
	s.registerMethodRoutes("/api/admin/projects/{project_id}/teams", s.adminProjectTeamsMethodNotAllowed,
		methodRoute{Method: http.MethodGet, Handler: s.handleAdminProjectTeamsGet},
		methodRoute{Method: http.MethodPost, Handler: s.handleAdminProjectTeamsPost},
	)
	s.registerMethodRoutes("/api/admin/projects/{project_id}/teams/{team_id}", s.adminProjectTeamMethodNotAllowed,
		methodRoute{Method: http.MethodPatch, Handler: s.handleAdminProjectTeamPatch},
		methodRoute{Method: http.MethodDelete, Handler: s.handleAdminProjectTeamDelete},
	)
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/projects/{project_id}/quota-increase", s.handleAdminProjectQuotaIncreasePost, s.adminProjectMethodNotAllowed("approval", http.MethodPost))
	s.mux.HandleFunc("/api/admin/projects/", s.handleAdminProjectNested)
	s.mux.HandleFunc("/api/admin/guardrail-policies", s.handleAdminGuardrailPolicies)
	s.mux.HandleFunc("/api/admin/guardrail-policies/test", s.handleAdminGuardrailPolicyTest)
	s.mux.HandleFunc("/api/admin/guardrail-policies/", s.handleAdminGuardrailPolicyItem)
	s.registerMethodRoutes("/api/admin/users", func(allowedMethods string) http.HandlerFunc {
		return s.adminMethodNotAllowed("identity", allowedMethods)
	},
		methodRoute{Method: http.MethodGet, Handler: s.handleAdminUsersGet},
		methodRoute{Method: http.MethodPost, Handler: s.handleAdminUsersPost},
	)
	userImportMethodNotAllowed := s.adminMethodNotAllowed("identity", http.MethodPost)
	s.mux.HandleFunc(http.MethodPost+" /api/admin/users/import", s.handleAdminUsersImportPost)
	s.mux.HandleFunc(http.MethodPatch+" /api/admin/users/import", userImportMethodNotAllowed)
	s.mux.HandleFunc(http.MethodDelete+" /api/admin/users/import", userImportMethodNotAllowed)
	s.registerDynamicMethodRoute(http.MethodPatch, "/api/admin/users/{user_id}", s.handleAdminUserPatch)
	s.registerDynamicMethodRoute(http.MethodDelete, "/api/admin/users/{user_id}", s.handleAdminUserDelete)
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/users/{user_id}/reset-password-email", s.handleAdminUserResetPasswordEmailPost, s.adminUserMethodNotAllowed(http.MethodPost))
	s.mux.HandleFunc("/api/admin/users/", s.handleAdminUserItem)
	s.mux.HandleFunc("/api/admin/provider-catalog", s.handleAdminProviderCatalog)
	s.mux.HandleFunc("/api/admin/provider-catalog/", s.handleAdminProviderCatalogItem)
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/provider-adapters", s.handleAdminProviderAdapters, s.adminMethodNotAllowed("providers", http.MethodGet))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/provider-account-oauth/openai/generate-auth-url", s.handleAdminOpenAIAccountOAuthGenerateAuthURL, s.adminMethodNotAllowed("provider", http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/provider-account-oauth/openai/exchange-code", s.handleAdminOpenAIAccountOAuthExchangeCode, s.adminMethodNotAllowed("provider", http.MethodPost))
	s.mux.HandleFunc("/api/admin/provider-account-oauth/openai/oauth/callback", s.handleOpenAIAccountOAuthCallback)
	s.registerMethodRoutes("/api/admin/api-keys", func(allowedMethods string) http.HandlerFunc {
		return s.adminMethodNotAllowed("api_key", allowedMethods)
	},
		methodRoute{Method: http.MethodGet, Handler: s.handleAdminAPIKeysGet},
		methodRoute{Method: http.MethodPost, Handler: s.handleAdminAPIKeysPost},
	)
	s.registerMethodRoutes("/api/admin/api-keys/{key_id}", s.adminAPIKeyMethodNotAllowed,
		methodRoute{Method: http.MethodPatch, Handler: s.handleAdminAPIKeyPatch},
		methodRoute{Method: http.MethodDelete, Handler: s.handleAdminAPIKeyDelete},
	)
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/api-keys/{key_id}/rotate", s.handleAdminAPIKeyRotatePost, s.adminAPIKeyMethodNotAllowed(http.MethodPost))
	s.mux.HandleFunc("/api/admin/api-keys/", s.handleAdminAPIKeyItem)
	s.mux.HandleFunc("/api/admin/analytics/credentials", s.handleAdminAnalyticsCredentials)
	s.mux.HandleFunc("/api/admin/analytics/credentials/", s.handleAdminAnalyticsCredentialItem)
	s.mux.HandleFunc("/api/admin/providers", s.handleAdminProviders)
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/providers/monitoring", s.handleAdminProviderMonitoring, s.adminMethodNotAllowed("provider", http.MethodGet))
	s.mux.HandleFunc("/api/admin/providers/", s.handleAdminProviderNested)
	s.mux.HandleFunc("/api/admin/provider-resources", s.handleAdminProviderResources)
	s.mux.HandleFunc("/api/admin/provider-resources/", s.handleAdminProviderResourceNested)
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/provider-models/import", s.handleAdminProviderModelImport, s.adminMethodNotAllowed("model", http.MethodPost))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/provider-models", s.handleAdminProviderModels, s.adminMethodNotAllowed("provider", http.MethodGet))
	s.mux.HandleFunc("/api/admin/provider-models/", s.handleAdminProviderModelItem)
	s.mux.HandleFunc("/api/admin/models", s.handleAdminModels)
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/models/restore-defaults", s.handleAdminModelsRestoreDefaults, s.adminMethodNotAllowed("model", http.MethodPost))
	s.mux.HandleFunc("/api/admin/models/", s.handleAdminModelItem)
	s.mux.HandleFunc("/api/admin/model-routing-policies/", s.handleAdminModelRoutingPolicy)
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/routing-policies/simulate", s.handleAdminRoutingPolicySimulation, s.adminMethodNotAllowed("routing", http.MethodPost))
	s.mux.HandleFunc("/api/admin/routing-policies/", s.handleAdminRoutingPolicyAction)
	s.mux.HandleFunc("/api/admin/routing-rules", s.handleAdminRoutes)
	s.mux.HandleFunc("/api/admin/routing-rules/", s.handleAdminRouteItem)
	s.mux.HandleFunc("/api/admin/resources/", s.handleAdminResources)
	s.mux.HandleFunc("/api/admin/sqlite/backups", s.handleAdminSQLiteBackups)
	s.mux.HandleFunc("/api/admin/sqlite/backups/", s.handleAdminSQLiteBackupItem)
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/billing/generate", s.handleAdminGenerateBilling, s.adminMethodNotAllowed("usage", http.MethodPost))
	s.registerBillingRoutes()
	s.mux.HandleFunc("/api/admin/export/", s.handleAdminExport)
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/usage/summary", s.handleAdminUsageSummary, s.adminMethodNotAllowed("usage", http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/usage/breakdown", s.handleAdminUsageBreakdown, s.adminMethodNotAllowed("usage", http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/usage/timeseries", s.handleAdminUsageTimeseries, s.adminMethodNotAllowed("usage", http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/audit/requests", s.handleAdminRequestLogs, s.adminMethodNotAllowed("audit", http.MethodGet))
	s.mux.HandleFunc("/api/admin/audit/requests/", s.handleAdminRequestDetail)
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/audit/image-jobs", s.handleAdminImageJobs, s.adminMethodNotAllowed("audit", http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/audit/events", s.handleAdminAuditEvents, s.adminMethodNotAllowed("admin_audit", http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/alerts", s.handleAdminAlerts, s.adminMethodNotAllowed("alert", http.MethodGet))
	s.mux.HandleFunc("/api/admin/alerts/", s.handleAdminAlertItem)
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/alert-deliveries", s.handleAdminAlertDeliveries, s.adminMethodNotAllowed("alert", http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/approvals", s.handleAdminApprovals, s.adminMethodNotAllowed("approval", http.MethodGet))
	s.mux.HandleFunc("/api/admin/approvals/", s.handleAdminApprovalItem)
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/system/db-status", s.handleAdminSystemDBStatus, plainTextMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/system/version", s.handleAdminSystemVersion, jsonMethodNotAllowed(http.MethodGet))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/system/update", s.handleAdminSystemUpdate, jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/system/rollback", s.handleAdminSystemRollback, jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodPost, "/api/admin/system/restart", s.handleAdminSystemRestart, jsonMethodNotAllowed(http.MethodPost))
	s.registerSingleMethodRoute(http.MethodGet, "/api/admin/system/rollback-versions", s.handleAdminRollbackVersions, jsonMethodNotAllowed(http.MethodGet))
}
