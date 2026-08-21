package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestUsageDailyUsesRequestedTimezoneWindow(t *testing.T) {
	store := NewMemoryStore()
	server := New(store)
	project := store.CreateProject(Project{ID: "prj_daily", Name: "Daily project", CostCenter: "CC-DAILY", Status: StatusActive})
	records := []UsageRecord{
		{
			ID:          "use_daily_previous",
			RequestID:   "req_daily_previous",
			ProjectID:   project.ID,
			APIKeyID:    "key_daily",
			ModelName:   "gpt-before",
			InputTokens: 10,
			TotalTokens: 10,
			CostUSD:     1,
			CreatedAt:   time.Date(2026, 3, 5, 15, 59, 59, 0, time.UTC),
		},
		{
			ID:           "use_daily_inside",
			RequestID:    "req_daily_inside",
			ProjectID:    project.ID,
			APIKeyID:     "key_daily",
			ModelName:    "gpt-today",
			InputTokens:  3,
			OutputTokens: 4,
			TotalTokens:  7,
			CostUSD:      0.25,
			CreatedAt:    time.Date(2026, 3, 5, 16, 0, 0, 0, time.UTC),
		},
		{
			ID:          "use_daily_next",
			RequestID:   "req_daily_next",
			ProjectID:   project.ID,
			APIKeyID:    "key_next",
			ModelName:   "gpt-next",
			TotalTokens: 20,
			CostUSD:     2,
			CreatedAt:   time.Date(2026, 3, 6, 16, 0, 0, 0, time.UTC),
		},
	}
	if err := store.db.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	requestLogs := []RequestLog{
		{
			ID:         "log_daily_previous",
			RequestID:  "req_daily_previous",
			ProjectID:  project.ID,
			APIKeyID:   "key_daily",
			StatusCode: http.StatusOK,
			CreatedAt:  time.Date(2026, 3, 5, 15, 59, 59, 0, time.UTC),
		},
		{
			ID:         "log_daily_inside",
			RequestID:  "req_daily_inside",
			ProjectID:  project.ID,
			APIKeyID:   "key_daily",
			StatusCode: http.StatusOK,
			CreatedAt:  time.Date(2026, 3, 5, 16, 0, 0, 0, time.UTC),
		},
		{
			ID:         "log_daily_error",
			RequestID:  "req_daily_error",
			ProjectID:  project.ID,
			APIKeyID:   "key_daily",
			StatusCode: http.StatusBadGateway,
			CreatedAt:  time.Date(2026, 3, 5, 17, 0, 0, 0, time.UTC),
		},
		{
			ID:         "log_daily_next",
			RequestID:  "req_daily_next",
			ProjectID:  project.ID,
			APIKeyID:   "key_next",
			StatusCode: http.StatusOK,
			CreatedAt:  time.Date(2026, 3, 6, 16, 0, 0, 0, time.UTC),
		},
	}
	if err := store.db.Create(&requestLogs).Error; err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 3, 6, 2, 30, 0, 0, time.UTC)
	daily, err := server.usageDailyForUser(t.Context(), AdminUser{ID: "usr_daily_admin", Role: "admin", Status: StatusActive}, "Asia/Shanghai", now)
	if err != nil {
		t.Fatal(err)
	}

	if got := daily["date"]; got != "2026-03-06" {
		t.Fatalf("date = %#v, want 2026-03-06", got)
	}
	if got := daily["window_start"]; got != "2026-03-05T16:00:00Z" {
		t.Fatalf("window_start = %#v, want Asia/Shanghai day start in UTC", got)
	}
	summary := daily["summary"].(map[string]any)
	if got := summary["request_count"]; got != int64(2) {
		t.Fatalf("request_count = %#v, want 2", got)
	}
	if got := summary["usage_record_count"]; got != int64(1) {
		t.Fatalf("usage_record_count = %#v, want 1", got)
	}
	if got := summary["errors"]; got != int64(1) {
		t.Fatalf("errors = %#v, want 1", got)
	}
	if got := summary["total_tokens"]; got != int64(7) {
		t.Fatalf("total_tokens = %#v, want 7", got)
	}
	breakdown := daily["breakdown"].(map[string]any)
	models := breakdown["models"].([]map[string]any)
	if len(models) != 1 || models[0]["id"] != "gpt-today" {
		t.Fatalf("model breakdown = %#v, want only today's record", models)
	}
	apiKeys := breakdown["api_keys"].([]map[string]any)
	if len(apiKeys) != 1 || apiKeys[0]["id"] != "key_daily" {
		t.Fatalf("api key breakdown = %#v, want key_daily", apiKeys)
	}
}

func TestUsageDailyUsesConfiguredDashboardTimezone(t *testing.T) {
	store := NewMemoryStore()
	server := New(store)
	store.CreateResource("settings", AdminResource{
		ID:     gatewaySettingsID,
		Status: StatusActive,
		Fields: map[string]any{dashboardTimezoneField: "Asia/Shanghai"},
	})

	daily, err := server.usageDailyForUser(t.Context(), AdminUser{ID: "usr_daily_admin", Role: "admin", Status: StatusActive}, "", time.Date(2026, 3, 6, 2, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if got := daily["timezone"]; got != "Asia/Shanghai" {
		t.Fatalf("timezone = %#v, want Asia/Shanghai", got)
	}
	if got := daily["window_start"]; got != "2026-03-05T16:00:00Z" {
		t.Fatalf("window_start = %#v, want dashboard timezone start", got)
	}
}

func TestUsageDailyRejectsInvalidTimezone(t *testing.T) {
	app := New(NewMemoryStore()).Handler()
	response := doJSON(t, app, http.MethodGet, "/api/admin/usage/daily?timezone=Nope/Nowhere", nil, "")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body, "invalid_usage_timezone") {
		t.Fatalf("invalid timezone response = %d %s, want invalid_usage_timezone", response.Code, response.Body)
	}
}

func TestGatewaySettingsRejectInvalidDashboardTimezone(t *testing.T) {
	store := NewMemoryStore()
	setting := AdminResource{
		ID:     gatewaySettingsID,
		Name:   "Gateway settings",
		Status: StatusActive,
		Fields: map[string]any{
			dashboardTimezoneField:        "Nope/Nowhere",
			syntheticDNSEnabledField:      false,
			syntheticDNSCIDRsField:        defaultSyntheticDNSCIDRs,
			syntheticDNSAllowPrivateField: false,
		},
	}
	response := doJSON(t, New(store).Handler(), http.MethodPatch, "/api/admin/resources/settings/"+gatewaySettingsID, setting, "")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body, "invalid_dashboard_timezone") {
		t.Fatalf("invalid dashboard timezone response = %d %s, want invalid_dashboard_timezone", response.Code, response.Body)
	}
}
