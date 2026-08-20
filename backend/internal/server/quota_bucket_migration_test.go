package server

import "testing"

func TestQuotaBucketMigrationPreservesUnknownAndUserScopedHistory(t *testing.T) {
	store := NewMemoryStore()
	if err := store.db.Exec("DROP TABLE quota_buckets").Error; err != nil {
		t.Fatal(err)
	}
	if err := store.db.Exec(`CREATE TABLE quota_buckets (
key_id TEXT NOT NULL,
scope TEXT NOT NULL,
bucket TEXT NOT NULL,
attributed_user_id TEXT,
requests INTEGER NOT NULL DEFAULT 0,
prompt_tokens INTEGER NOT NULL DEFAULT 0,
completion_tokens INTEGER NOT NULL DEFAULT 0,
total_tokens INTEGER NOT NULL DEFAULT 0,
cost_usd REAL NOT NULL DEFAULT 0,
PRIMARY KEY (key_id, scope, bucket)
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.db.Exec("INSERT INTO quota_buckets (key_id, scope, bucket, requests, total_tokens) VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)", "key_legacy", "day", "2026-08-20", 4, 12, "user:usr_legacy", "day", "2026-08-20", 2, 8).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensureQuotaBucketAttributionSchema(store.db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	var keyRow, userRow QuotaBucket
	if err := store.db.Where("key_id = ?", "key_legacy").First(&keyRow).Error; err != nil {
		t.Fatal(err)
	}
	if keyRow.AttributedUserID != unattributedQuotaUserID || keyRow.TotalTokens != 12 {
		t.Fatalf("legacy key history = %+v", keyRow)
	}
	if err := store.db.Where("key_id = ?", "user:usr_legacy").First(&userRow).Error; err != nil {
		t.Fatal(err)
	}
	if userRow.AttributedUserID != "usr_legacy" || userRow.TotalTokens != 8 {
		t.Fatalf("legacy user history = %+v", userRow)
	}
	aggregate, err := store.aggregateUserQuotaCounter(store.db, "usr_legacy", "day", "2026-08-20")
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.TotalTokens != 0 {
		t.Fatalf("unattributed legacy key history was assigned to user: %+v", aggregate)
	}
}
