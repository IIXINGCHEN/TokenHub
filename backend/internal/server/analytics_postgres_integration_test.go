//go:build integration

package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func testPostgresAnalyticsLegacySequenceMigration(t *testing.T, adminStore *GormStore, config Config) {
	t.Helper()
	schema := fmt.Sprintf("tokenhub_e2e_analytics_upgrade_%d", time.Now().UnixNano())
	if err := adminStore.db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("create analytics upgrade schema: %v", err)
	}
	defer func() {
		if err := adminStore.db.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("drop analytics upgrade schema: %v", err)
		}
	}()
	parsedURL, err := url.Parse(config.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schema)
	parsedURL.RawQuery = query.Encode()
	config.DatabaseURL = parsedURL.String()

	closeStore := func(store *GormStore) {
		t.Helper()
		for _, database := range []interface{ DB() (*sql.DB, error) }{store.db, store.analyticsDB} {
			sqlDB, databaseErr := database.DB()
			if databaseErr == nil {
				_ = sqlDB.Close()
			}
		}
	}
	legacyStore, err := NewStoreWithDialect(config.DatabaseURL, config)
	if err != nil {
		t.Fatalf("create legacy analytics schema: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	projectID := "project_legacy_analytics"
	logs := []RequestLog{
		{
			ID: "log_legacy_in_window", RequestID: "req_legacy_in_window", ProjectID: projectID,
			ModelName: "gpt-legacy", StatusCode: http.StatusOK, CreatedAt: now.Add(-time.Hour),
		},
		{
			ID: "log_legacy_after_to", RequestID: "req_legacy_after_to", ProjectID: projectID,
			ModelName: "gpt-legacy", StatusCode: http.StatusOK, CreatedAt: now.Add(time.Hour),
		},
	}
	if err := legacyStore.db.Create(&logs).Error; err != nil {
		closeStore(legacyStore)
		t.Fatal(err)
	}
	// Simulate the pre-upgrade allocator with commit order opposite event order.
	if err := legacyStore.db.Model(&RequestLog{}).
		Where("id = ?", logs[0].ID).Update("commit_sequence", 2).Error; err != nil {
		closeStore(legacyStore)
		t.Fatal(err)
	}
	if err := legacyStore.db.Model(&RequestLog{}).
		Where("id = ?", logs[1].ID).Update("commit_sequence", 1).Error; err != nil {
		closeStore(legacyStore)
		t.Fatal(err)
	}
	if err := legacyStore.db.Model(&AnalyticsSequence{}).
		Where("name = ?", requestLogSequenceName).Update("last_value", 2).Error; err != nil {
		closeStore(legacyStore)
		t.Fatal(err)
	}
	closeStore(legacyStore)

	upgradedStore, err := NewStoreWithDialect(config.DatabaseURL, config)
	if err != nil {
		t.Fatalf("upgrade legacy analytics schema: %v", err)
	}
	defer closeStore(upgradedStore)
	page, err := upgradedStore.QueryTokenCostPage(t.Context(), TokenCostQuery{
		From: now.Add(-2 * time.Hour), To: now, ProjectID: projectID,
		Granularity: "request", Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.Rows[0].RequestID != logs[0].RequestID {
		t.Fatalf("legacy time window after PostgreSQL upgrade = %#v", page)
	}
	var migrated []RequestLog
	if err := upgradedStore.db.Where("id IN ?", []string{logs[0].ID, logs[1].ID}).
		Order("created_at ASC, id ASC").Find(&migrated).Error; err != nil {
		t.Fatal(err)
	}
	if len(migrated) != 2 || migrated[0].CommitSequence <= 0 ||
		migrated[0].CommitSequence >= migrated[1].CommitSequence || page.Checkpoint != migrated[0].CommitSequence {
		t.Fatalf("legacy PostgreSQL sequences were not ordered by event time: rows=%#v checkpoint=%d", migrated, page.Checkpoint)
	}
	var marker AnalyticsSequence
	if err := upgradedStore.db.First(&marker, "name = ?", requestLogSequenceName).Error; err != nil {
		t.Fatal(err)
	}
	if marker.LastValue != -1 {
		t.Fatalf("legacy PostgreSQL migration marker = %d, want -1", marker.LastValue)
	}
	if err := backfillRequestLogCommitSequence(upgradedStore.db, "postgres"); err != nil {
		t.Fatalf("repeat legacy PostgreSQL migration: %v", err)
	}
	var repeated []RequestLog
	if err := upgradedStore.db.Where("id IN ?", []string{logs[0].ID, logs[1].ID}).
		Order("created_at ASC, id ASC").Find(&repeated).Error; err != nil {
		t.Fatal(err)
	}
	if len(repeated) != 2 || repeated[0].CommitSequence != migrated[0].CommitSequence ||
		repeated[1].CommitSequence != migrated[1].CommitSequence {
		t.Fatalf("legacy PostgreSQL migration was not idempotent: first=%#v repeated=%#v", migrated, repeated)
	}
}
