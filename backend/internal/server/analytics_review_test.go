package server

import (
	"database/sql"
	"encoding/csv"
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
		ModelName: "gpt-snapshot", StatusCode: http.StatusOK, CreatedAt: base.Add(-time.Second),
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
	foundConcurrent := false
	seenDedupeKeys := map[string]bool{}
	for _, row := range incremental.Data {
		if row.RequestID == "req_snapshot_concurrent" {
			foundConcurrent = true
		}
		if row.DedupeKey == "" || seenDedupeKeys[row.DedupeKey] {
			t.Fatalf("incremental replay has invalid dedupe key: %#v", incremental.Data)
		}
		seenDedupeKeys[row.DedupeKey] = true
	}
	if !incremental.Query.IncrementalReplay || !foundConcurrent {
		t.Fatalf("incremental replay lost delayed commit: %#v", incremental)
	}
}

func TestTokenCostIncrementalReplayUsesBoundedOverlap(t *testing.T) {
	store := NewMemoryStore()
	app := New(store).Handler()
	token := createAnalyticsCredentialToken(t, app, map[string]any{
		"name": "bounded-replay-agent", "scope_type": AnalyticsScopeOrganization,
	})
	now := time.Now().UTC().Truncate(time.Second)
	initialThrough := now.Add(-48 * time.Hour)
	initialFrom := initialThrough.Add(-30 * 24 * time.Hour)
	watermarkAt := initialThrough.Add(-time.Hour)
	createAnalyticsRequest(t, store, RequestLog{
		ID: "log_bounded_replay", RequestID: "req_bounded_replay", ProjectID: "project_bounded_replay",
		ModelName: "gpt-bounded-replay", StatusCode: http.StatusOK, CreatedAt: watermarkAt,
	}, nil)

	initialQuery := url.Values{
		"from": {initialFrom.Format(time.RFC3339Nano)},
		"to":   {initialThrough.Format(time.RFC3339Nano)},
	}
	initialResponse := doJSON(t, app, http.MethodGet, "/api/v1/analytics/token-costs?"+initialQuery.Encode(), nil, token)
	if initialResponse.Code != http.StatusOK {
		t.Fatalf("initial bounded replay query: %d %s", initialResponse.Code, initialResponse.Body)
	}
	var initial TokenCostResponse
	if err := json.Unmarshal([]byte(initialResponse.Body), &initial); err != nil {
		t.Fatal(err)
	}

	incrementalQuery := url.Values{
		"after": {initial.Watermark},
		"to":    {now.Format(time.RFC3339Nano)},
	}
	incrementalResponse := doJSON(t, app, http.MethodGet, "/api/v1/analytics/token-costs?"+incrementalQuery.Encode(), nil, token)
	if incrementalResponse.Code != http.StatusOK {
		t.Fatalf("bounded incremental replay: %d %s", incrementalResponse.Code, incrementalResponse.Body)
	}
	var incremental TokenCostResponse
	if err := json.Unmarshal([]byte(incrementalResponse.Body), &incremental); err != nil {
		t.Fatal(err)
	}
	wantFrom := watermarkAt.Add(-tokenCostReplayOverlap).Format(time.RFC3339Nano)
	if incremental.Query.From != wantFrom || !incremental.Query.IncrementalReplay || len(incremental.Data) != 1 {
		t.Fatalf("bounded incremental replay = %#v, want from %s", incremental, wantFrom)
	}
}

func TestTokenCostCSVNeutralizesRejectedModelFormula(t *testing.T) {
	store := NewMemoryStore()
	project := store.CreateProject(Project{Name: "CSV Formula Project", Status: StatusActive})
	app := New(store).Handler()
	token := createAnalyticsCredentialToken(t, app, map[string]any{
		"name": "csv-formula-agent", "scope_type": AnalyticsScopeOrganization,
	})
	maliciousModel := `=HYPERLINK("https://attacker.invalid","open")`
	store.RecordRejectedRequest(
		project, APIKey{ID: "key_csv_formula", OwnerUserID: "user_csv_formula"}, maliciousModel,
		false, http.StatusBadRequest, "model_not_found", "127.0.0.1", "analytics-test",
	)

	response := doJSON(t, app, http.MethodGet, "/api/v1/analytics/token-costs?format=csv&status=error", nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("CSV formula export: %d %s", response.Code, response.Body)
	}
	records, err := csv.NewReader(strings.NewReader(response.Body)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV rows = %#v, want header plus rejected request", records)
	}
	modelColumn := -1
	for index, column := range records[0] {
		if column == "model" {
			modelColumn = index
			break
		}
	}
	if modelColumn < 0 || records[1][modelColumn] != "'"+maliciousModel {
		t.Fatalf("CSV model cell was not neutralized: %#v", records)
	}
}

func TestLegacyRequestLogAttributionMigration(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "legacy-analytics-attribution.db")
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE projects (
			id text primary key, name text, team_id text, owner_user_id text,
			status text, created_at datetime, updated_at datetime
		)`,
		`CREATE TABLE api_keys (
			id text primary key, project_id text, owner_user_id text, metadata text,
			status text, created_at datetime
		)`,
		`CREATE TABLE request_logs (
			id text primary key, request_id text, project_id text, api_key_id text,
			model_name text, status_code integer, created_at datetime
		)`,
		`CREATE TABLE usage_records (
			id text primary key, request_id text, attributed_user_id text, created_at datetime
		)`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if _, err := db.Exec(
		`INSERT INTO projects (id, name, owner_user_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"project_legacy_attribution", "Legacy Attribution", "user_project_owner", StatusActive, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO api_keys (id, project_id, owner_user_id, status, created_at) VALUES (?, ?, ?, ?, ?)`,
		"key_legacy_attribution", "project_legacy_attribution", "user_key_owner", StatusActive, now,
	); err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]any{
		{"log_legacy_usage", "req_legacy_usage", "key_legacy_attribution"},
		{"log_legacy_key", "req_legacy_key", "key_legacy_attribution"},
		{"log_legacy_project", "req_legacy_project", "missing_key"},
	} {
		if _, err := db.Exec(
			`INSERT INTO request_logs (id, request_id, project_id, api_key_id, model_name, status_code, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			values[0], values[1], "project_legacy_attribution", values[2], "legacy-model", http.StatusBadGateway, now,
		); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO usage_records (id, request_id, attributed_user_id, created_at) VALUES (?, ?, ?, ?)`,
		"use_legacy_attribution", "req_legacy_usage", "user_usage_owner", now,
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewSQLiteStore("sqlite://" + databasePath)
	if err != nil {
		t.Fatal(err)
	}
	var logs []RequestLog
	if err := store.db.Where("request_id LIKE ?", "req_legacy_%").Order("request_id asc").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, requestLog := range logs {
		got[requestLog.RequestID] = requestLog.AttributedUserID
	}
	want := map[string]string{
		"req_legacy_usage":   "user_usage_owner",
		"req_legacy_key":     "user_key_owner",
		"req_legacy_project": "user_project_owner",
	}
	for requestID, userID := range want {
		if got[requestID] != userID {
			t.Fatalf("legacy attribution for %s = %q, want %q; all=%#v", requestID, got[requestID], userID, got)
		}
	}
}
