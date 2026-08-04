package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

func (s *GormStore) CreateAnalyticsCredential(credential AnalyticsCredential, rawSecret string) (AnalyticsCredential, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	credential.Name = strings.TrimSpace(credential.Name)
	credential.ScopeType = strings.ToLower(strings.TrimSpace(credential.ScopeType))
	credential.ProjectID = strings.TrimSpace(credential.ProjectID)
	if credential.Name == "" {
		return AnalyticsCredential{}, "", NewHTTPError(http.StatusBadRequest, "invalid_analytics_credential", "Analytics credential name is required")
	}
	if credential.ScopeType != AnalyticsScopeOrganization && credential.ScopeType != AnalyticsScopeProject {
		return AnalyticsCredential{}, "", NewHTTPError(http.StatusBadRequest, "invalid_analytics_scope", "Analytics scope_type must be organization or project")
	}
	if credential.ScopeType == AnalyticsScopeProject {
		if credential.ProjectID == "" {
			return AnalyticsCredential{}, "", NewHTTPError(http.StatusBadRequest, "invalid_analytics_scope", "project_id is required for a project analytics credential")
		}
		if err := s.db.First(&Project{}, "id = ?", credential.ProjectID).Error; err != nil {
			return AnalyticsCredential{}, "", notFound(err, "project_not_found", "Project not found")
		}
	} else {
		credential.ProjectID = ""
	}
	if credential.ExpiresAt != nil && !credential.ExpiresAt.After(time.Now().UTC()) {
		return AnalyticsCredential{}, "", NewHTTPError(http.StatusBadRequest, "invalid_analytics_expiry", "expires_at must be in the future")
	}
	if rawSecret == "" {
		rawSecret = GenerateAPIKeyWithOptions("tha_", DefaultAPIKeyRandomLength)
	}
	prefix, suffix := PrefixSuffix(rawSecret)
	now := time.Now().UTC()
	credential.ID = NewID("acred")
	credential.KeyHash = HashSecret(rawSecret)
	credential.KeyPrefix = prefix
	credential.KeySuffix = suffix
	credential.Status = StatusActive
	credential.CreatedAt = now
	credential.UpdatedAt = now
	if err := s.db.Create(&credential).Error; err != nil {
		return AnalyticsCredential{}, "", writeConflict(err, "analytics_credential_conflict", "Analytics credential already exists")
	}
	return credential, rawSecret, nil
}

func (s *GormStore) ListAnalyticsCredentials() []AnalyticsCredential {
	var credentials []AnalyticsCredential
	_ = s.db.Order("created_at asc").Find(&credentials).Error
	return credentials
}

func (s *GormStore) RevokeAnalyticsCredential(id string) (AnalyticsCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var credential AnalyticsCredential
	if err := s.db.First(&credential, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return AnalyticsCredential{}, notFound(err, "analytics_credential_not_found", "Analytics credential not found")
	}
	credential.Status = StatusRevoked
	credential.UpdatedAt = time.Now().UTC()
	if err := s.db.Save(&credential).Error; err != nil {
		return AnalyticsCredential{}, err
	}
	return credential, nil
}

func (s *GormStore) ValidateAnalyticsCredential(rawSecret string) (AnalyticsCredential, error) {
	var credential AnalyticsCredential
	if err := s.db.First(&credential, "key_hash = ?", HashSecret(strings.TrimSpace(rawSecret))).Error; err != nil {
		return AnalyticsCredential{}, NewHTTPError(http.StatusUnauthorized, "invalid_analytics_credential", "Invalid analytics credential")
	}
	if credential.Status != StatusActive {
		return AnalyticsCredential{}, NewHTTPError(http.StatusUnauthorized, "invalid_analytics_credential", "Invalid analytics credential")
	}
	now := time.Now().UTC()
	if credential.ExpiresAt != nil && !credential.ExpiresAt.After(now) {
		return AnalyticsCredential{}, NewHTTPError(http.StatusUnauthorized, "analytics_credential_expired", "Analytics credential has expired")
	}
	if err := s.db.Model(&AnalyticsCredential{}).Where("id = ?", credential.ID).Updates(map[string]any{
		"last_used_at": now,
		"updated_at":   now,
	}).Error; err != nil {
		return AnalyticsCredential{}, err
	}
	credential.LastUsedAt = &now
	credential.UpdatedAt = now
	return credential, nil
}

type tokenCostDatabaseRow struct {
	Bucket            string
	RequestID         string
	OccurredAt        time.Time
	ProjectID         string
	UserID            string
	APIKeyID          string
	ProviderID        string
	Model             string
	Status            string
	StatusCode        int
	RequestCount      int64
	ErrorCount        int64
	InputTokens       int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	OutputTokens      int64
	ReasoningTokens   int64
	TotalTokens       int64
	EstimatedCostUSD  float64
}

func (s *GormStore) QueryTokenCosts(ctx context.Context, query TokenCostQuery) ([]TokenCostRow, bool, error) {
	if query.Granularity != "request" {
		return s.queryAggregatedTokenCosts(ctx, query)
	}
	db := s.tokenCostRequestQuery(ctx, query).
		Select(`rl.request_id,
			rl.created_at AS occurred_at,
			rl.project_id,
			COALESCE(u.user_id, '') AS user_id,
			rl.api_key_id,
			rl.provider_id,
			rl.model_name AS model,
			rl.status_code,
			COALESCE(u.input_tokens, 0) AS input_tokens,
			COALESCE(u.cached_input_tokens, 0) AS cached_input_tokens,
			COALESCE(u.cache_write_tokens, 0) AS cache_write_tokens,
			COALESCE(u.output_tokens, 0) AS output_tokens,
			COALESCE(u.reasoning_tokens, 0) AS reasoning_tokens,
			COALESCE(u.total_tokens, 0) AS total_tokens,
			COALESCE(u.estimated_cost_usd, 0) AS estimated_cost_usd`)
	limit := normalizedTokenCostLimit(query.Limit)
	var databaseRows []tokenCostDatabaseRow
	if err := db.Order("rl.created_at ASC, rl.request_id ASC").Limit(limit + 1).Scan(&databaseRows).Error; err != nil {
		return nil, false, err
	}
	return tokenCostRows(databaseRows, limit, false)
}

func (s *GormStore) tokenCostRequestQuery(ctx context.Context, query TokenCostQuery) *gorm.DB {
	usage := s.analyticsDB.WithContext(ctx).Table("usage_records").
		Select(`request_id,
			MAX(attributed_user_id) AS user_id,
			SUM(input_tokens) AS input_tokens,
			SUM(cached_input_tokens) AS cached_input_tokens,
			SUM(cache_write_tokens) AS cache_write_tokens,
			SUM(output_tokens) AS output_tokens,
			SUM(reasoning_tokens) AS reasoning_tokens,
			SUM(total_tokens) AS total_tokens,
			SUM(cost_usd) AS estimated_cost_usd`).
		Where("created_at >= ? AND created_at < ?", query.From, query.To).
		Group("request_id")
	if query.ProjectID != "" {
		usage = usage.Where("project_id = ?", query.ProjectID)
	}
	if query.UserID != "" {
		usage = usage.Where("attributed_user_id = ?", query.UserID)
	}
	if query.APIKeyID != "" {
		usage = usage.Where("api_key_id = ?", query.APIKeyID)
	}
	if query.ProviderID != "" {
		usage = usage.Where("provider_id = ?", query.ProviderID)
	}
	if query.Model != "" {
		usage = usage.Where("model_name = ?", query.Model)
	}

	db := s.analyticsDB.WithContext(ctx).Table("request_logs AS rl").
		Joins("LEFT JOIN (?) AS u ON u.request_id = rl.request_id", usage).
		Where("rl.created_at >= ? AND rl.created_at < ?", query.From, query.To).
		Where("rl.project_id <> ?", "admin_playground")
	if query.ProjectID != "" {
		db = db.Where("rl.project_id = ?", query.ProjectID)
	}
	if !query.AfterAt.IsZero() {
		db = db.Where("(rl.created_at > ? OR (rl.created_at = ? AND rl.request_id > ?))", query.AfterAt, query.AfterAt, query.AfterID)
	}
	if query.UserID != "" {
		db = db.Where("COALESCE(u.user_id, '') = ?", query.UserID)
	}
	if query.APIKeyID != "" {
		db = db.Where("rl.api_key_id = ?", query.APIKeyID)
	}
	if query.ProviderID != "" {
		db = db.Where("rl.provider_id = ?", query.ProviderID)
	}
	if query.Model != "" {
		db = db.Where("rl.model_name = ?", query.Model)
	}
	switch query.Status {
	case TokenCostStatusSuccess:
		db = db.Where("rl.status_code < ?", http.StatusBadRequest)
	case TokenCostStatusError:
		db = db.Where("rl.status_code >= ?", http.StatusBadRequest)
	}
	return db
}

func (s *GormStore) queryAggregatedTokenCosts(ctx context.Context, query TokenCostQuery) ([]TokenCostRow, bool, error) {
	selects := make([]string, 0, len(query.GroupBy)+10)
	groups := make([]string, 0, len(query.GroupBy)+1)
	if query.Granularity != "none" {
		expression := s.tokenCostBucketExpression(query.Granularity)
		selects = append(selects, expression+" AS bucket")
		groups = append(groups, expression)
	}
	for _, dimension := range query.GroupBy {
		expression, selection := tokenCostDimensionSQL(dimension)
		selects = append(selects, selection)
		groups = append(groups, expression)
	}
	selects = append(selects,
		"COUNT(*) AS request_count",
		"SUM(CASE WHEN rl.status_code >= 400 THEN 1 ELSE 0 END) AS error_count",
		"COALESCE(SUM(u.input_tokens), 0) AS input_tokens",
		"COALESCE(SUM(u.cached_input_tokens), 0) AS cached_input_tokens",
		"COALESCE(SUM(u.cache_write_tokens), 0) AS cache_write_tokens",
		"COALESCE(SUM(u.output_tokens), 0) AS output_tokens",
		"COALESCE(SUM(u.reasoning_tokens), 0) AS reasoning_tokens",
		"COALESCE(SUM(u.total_tokens), 0) AS total_tokens",
		"COALESCE(SUM(u.estimated_cost_usd), 0) AS estimated_cost_usd",
	)
	db := s.tokenCostRequestQuery(ctx, query).Select(strings.Join(selects, ", "))
	if len(groups) > 0 {
		db = db.Group(strings.Join(groups, ", ")).Order(strings.Join(groups, ", "))
	}
	limit := normalizedTokenCostLimit(query.Limit)
	var databaseRows []tokenCostDatabaseRow
	if err := db.Offset(query.Offset).Limit(limit + 1).Scan(&databaseRows).Error; err != nil {
		return nil, false, err
	}
	return tokenCostRows(databaseRows, limit, true)
}

func (s *GormStore) tokenCostBucketExpression(granularity string) string {
	if s.dbDriver == "postgres" {
		switch granularity {
		case "hour":
			return `to_char(date_trunc('hour', rl.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD"T"HH24:00:00"Z"')`
		case "day":
			return `to_char(date_trunc('day', rl.created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD')`
		default:
			return `to_char(date_trunc('month', rl.created_at AT TIME ZONE 'UTC'), 'YYYY-MM')`
		}
	}
	switch granularity {
	case "hour":
		return `strftime('%Y-%m-%dT%H:00:00Z', rl.created_at)`
	case "day":
		return `strftime('%Y-%m-%d', rl.created_at)`
	default:
		return `strftime('%Y-%m', rl.created_at)`
	}
}

func tokenCostDimensionSQL(dimension string) (string, string) {
	switch dimension {
	case "project":
		return "rl.project_id", "rl.project_id AS project_id"
	case "user":
		return "COALESCE(u.user_id, '')", "COALESCE(u.user_id, '') AS user_id"
	case "api_key":
		return "rl.api_key_id", "rl.api_key_id AS api_key_id"
	case "provider":
		return "rl.provider_id", "rl.provider_id AS provider_id"
	case "model":
		return "rl.model_name", "rl.model_name AS model"
	default:
		expression := "CASE WHEN rl.status_code >= 400 THEN 'error' ELSE 'success' END"
		return expression, expression + " AS status"
	}
}

func normalizedTokenCostLimit(limit int) int {
	if limit <= 0 {
		return defaultTokenCostLimit
	}
	return limit
}

func tokenCostRows(databaseRows []tokenCostDatabaseRow, limit int, aggregated bool) ([]TokenCostRow, bool, error) {
	hasMore := len(databaseRows) > limit
	if hasMore {
		databaseRows = databaseRows[:limit]
	}
	rows := make([]TokenCostRow, 0, len(databaseRows))
	for _, row := range databaseRows {
		status := row.Status
		requestCount := row.RequestCount
		errorCount := row.ErrorCount
		if !aggregated {
			status = TokenCostStatusSuccess
			requestCount = 1
			if row.StatusCode >= http.StatusBadRequest {
				status = TokenCostStatusError
				errorCount = 1
			}
		}
		occurredAt := ""
		if !row.OccurredAt.IsZero() {
			occurredAt = row.OccurredAt.UTC().Format(time.RFC3339Nano)
		}
		rows = append(rows, TokenCostRow{
			Bucket: row.Bucket, RequestID: row.RequestID, OccurredAt: occurredAt,
			ProjectID: row.ProjectID, UserID: row.UserID, APIKeyID: row.APIKeyID,
			ProviderID: row.ProviderID, Model: row.Model, Status: status, StatusCode: row.StatusCode,
			Metrics: TokenCostMetrics{
				RequestCount: requestCount, ErrorCount: errorCount, InputTokens: row.InputTokens,
				CachedInputTokens: row.CachedInputTokens, CacheWriteTokens: row.CacheWriteTokens,
				OutputTokens: row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
				TotalTokens: row.TotalTokens, EstimatedCostUSD: row.EstimatedCostUSD,
			},
		})
	}
	return rows, hasMore, nil
}

func (s *GormStore) TokenCostWatermark(ctx context.Context, query TokenCostQuery) (time.Time, string, error) {
	var row struct {
		OccurredAt time.Time
		RequestID  string
	}
	err := s.tokenCostRequestQuery(ctx, query).
		Select("rl.created_at AS occurred_at, rl.request_id").
		Order("rl.created_at DESC, rl.request_id DESC").
		Limit(1).
		Scan(&row).Error
	return row.OccurredAt, row.RequestID, err
}
