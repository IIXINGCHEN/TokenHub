package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestTokenCostAnalyticsAttributesRejectedRequestsWithoutUsage(t *testing.T) {
	store := NewMemoryStore()
	project := store.CreateProject(Project{Name: "Rejected Attribution Project", Status: StatusActive})
	app := New(store).Handler()
	token := createAnalyticsCredentialToken(t, app, map[string]any{
		"name": "rejected-attribution-agent", "scope_type": AnalyticsScopeOrganization,
	})

	requestID := store.RecordRejectedRequest(
		project,
		APIKey{ID: "key_rejected_attribution", OwnerUserID: "user_rejected_attribution"},
		"missing-model", false, http.StatusBadRequest, "model_not_found", "127.0.0.1", "analytics-test",
	)
	store.RecordRejectedRequest(
		project,
		APIKey{ID: "key_other_rejected", OwnerUserID: "user_other_rejected"},
		"other-missing-model", false, http.StatusBadRequest, "model_not_found", "127.0.0.1", "analytics-test",
	)
	if records := store.ListUsageRecords(); len(records) != 0 {
		t.Fatalf("rejected requests unexpectedly created usage rows: %#v", records)
	}
	logs := store.ListRequestLogs()
	foundAttributedLog := false
	for _, requestLog := range logs {
		if requestLog.RequestID == requestID {
			foundAttributedLog = requestLog.AttributedUserID == "user_rejected_attribution"
		}
	}
	if !foundAttributedLog {
		t.Fatalf("rejected request did not persist immutable user attribution: %#v", logs)
	}

	query := url.Values{
		"user_id":     {"user_rejected_attribution"},
		"status":      {TokenCostStatusError},
		"granularity": {"none"},
		"group_by":    {"user"},
	}
	response := doJSON(t, app, http.MethodGet, "/api/v1/analytics/token-costs?"+query.Encode(), nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("query rejected request attribution: %d %s", response.Code, response.Body)
	}
	var payload TokenCostResponse
	if err := json.Unmarshal([]byte(response.Body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].UserID != "user_rejected_attribution" ||
		payload.Data[0].Metrics.RequestCount != 1 || payload.Data[0].Metrics.ErrorCount != 1 {
		t.Fatalf("rejected request user grouping/filtering = %#v", payload.Data)
	}
}

func TestTokenCostWatermarkSharesRowsDatabaseSnapshot(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "analytics-snapshot.db")
	store, err := NewSQLiteStoreWithConfig(databaseURL, Config{SecretKey: "analytics-snapshot-test"})
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := store.CreateAnalyticsCredential(AnalyticsCredential{
		Name: "snapshot-agent", ScopeType: AnalyticsScopeOrganization,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	app := New(store).Handler()
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	through := base.Add(time.Hour)
	createAnalyticsRequest(t, store, RequestLog{
		ID: "log_snapshot_initial", RequestID: "req_snapshot_initial", ProjectID: "project_snapshot",
		ModelName: "gpt-snapshot", StatusCode: http.StatusOK, CreatedAt: base,
	}, nil)

	rowsRead := make(chan struct{})
	continueSnapshot := make(chan struct{})
	var pauseOnce sync.Once
	callbackName := "test:pause_token_cost_snapshot"
	if err := store.analyticsDB.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if !strings.Contains(strings.ToLower(tx.Statement.SQL.String()), "order by rl.created_at asc") {
			return
		}
		pauseOnce.Do(func() {
			close(rowsRead)
			<-continueSnapshot
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.analyticsDB.Callback().Query().Remove(callbackName)
	})

	initialQuery := url.Values{
		"from": {base.Add(-time.Minute).Format(time.RFC3339Nano)},
		"to":   {through.Format(time.RFC3339Nano)},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/token-costs?"+initialQuery.Encode(), nil)
	request.Header.Set("authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	requestDone := make(chan struct{})
	go func() {
		app.ServeHTTP(recorder, request)
		close(requestDone)
	}()

	select {
	case <-rowsRead:
	case <-time.After(5 * time.Second):
		close(continueSnapshot)
		t.Fatal("analytics row query did not reach the snapshot pause")
	}
	createErr := store.db.Create(&RequestLog{
		ID: "log_snapshot_concurrent", RequestID: "req_snapshot_concurrent", ProjectID: "project_snapshot",
		ModelName: "gpt-snapshot", StatusCode: http.StatusOK, CreatedAt: base.Add(time.Second),
	}).Error
	close(continueSnapshot)
	if createErr != nil {
		t.Fatalf("create concurrent request: %v", createErr)
	}
	select {
	case <-requestDone:
	case <-time.After(5 * time.Second):
		t.Fatal("analytics snapshot query did not finish")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("snapshot response: %d %s", recorder.Code, recorder.Body.String())
	}
	var initial TokenCostResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial.Data) != 1 || initial.Data[0].RequestID != "req_snapshot_initial" {
		t.Fatalf("snapshot rows included concurrent write: %#v", initial.Data)
	}
	watermark, err := decodeTokenCostCursor(initial.Watermark)
	if err != nil {
		t.Fatalf("decode snapshot watermark: %v", err)
	}
	if watermark.AfterID != "req_snapshot_initial" {
		t.Fatalf("watermark advanced past exported rows: %#v", watermark)
	}

	incrementalQuery := url.Values{
		"after": {initial.Watermark},
		"to":    {through.Format(time.RFC3339Nano)},
	}
	incrementalResponse := doJSON(t, app, http.MethodGet, "/api/v1/analytics/token-costs?"+incrementalQuery.Encode(), nil, token)
	if incrementalResponse.Code != http.StatusOK {
		t.Fatalf("incremental snapshot response: %d %s", incrementalResponse.Code, incrementalResponse.Body)
	}
	var incremental TokenCostResponse
	if err := json.Unmarshal([]byte(incrementalResponse.Body), &incremental); err != nil {
		t.Fatal(err)
	}
	if len(incremental.Data) != 1 || incremental.Data[0].RequestID != "req_snapshot_concurrent" {
		t.Fatalf("incremental pull lost concurrent request: %#v", incremental.Data)
	}
}
