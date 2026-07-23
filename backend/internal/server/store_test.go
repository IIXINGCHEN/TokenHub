package server

import (
	"math"
	"testing"
)

func TestPriceUsageAppliesConfiguredCacheReadPrice(t *testing.T) {
	model := Model{
		Modality:               "chat",
		InputPriceUSDPer1M:     2,
		CacheReadPriceUSDPer1M: 0.5,
		OutputPriceUSDPer1M:    8,
	}
	usage := priceUsage(model, Usage{
		PromptTokens:      1000,
		CachedInputTokens: 400,
		CompletionTokens:  100,
	})

	if math.Abs(usage.CostUSD-0.0022) > 1e-12 {
		t.Fatalf("cost = %.12f, want 0.0022", usage.CostUSD)
	}
	if usage.TotalTokens != 1100 {
		t.Fatalf("total tokens = %d, want 1100", usage.TotalTokens)
	}
}

func TestEffectiveCacheReadPriceUsesCategoryEstimateWhenUnconfigured(t *testing.T) {
	tests := []struct {
		name  string
		model Model
		want  float64
	}{
		{
			name:  "default ten percent",
			model: Model{Name: "gpt-test", Category: "openai", InputPriceUSDPer1M: 2},
			want:  0.2,
		},
		{
			name:  "deepseek two percent",
			model: Model{Name: "deepseek-test", Category: "deepseek", InputPriceUSDPer1M: 2},
			want:  0.04,
		},
		{
			name:  "deepseek v4 pro current ratio",
			model: Model{Name: "deepseek-v4-pro", Category: "deepseek", InputPriceUSDPer1M: 2},
			want:  2.0 / 120,
		},
		{
			name: "legacy metadata remains supported",
			model: Model{
				Name:               "legacy",
				InputPriceUSDPer1M: 2,
				Metadata:           map[string]string{"cached_input_price_usd_per_1m": "0.3"},
			},
			want: 0.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveCacheReadPriceUSDPer1M(tt.model); math.Abs(got-tt.want) > 1e-12 {
				t.Fatalf("effective cache price = %.12f, want %.12f", got, tt.want)
			}
		})
	}
}

func TestDeleteAdminUserProtectsLastActivePlatformAdmin(t *testing.T) {
	store := NewMemoryStore()
	admin := createTestAdminUser(t, store, "only-admin", "admin")
	member := createTestAdminUser(t, store, "member", "user")

	if err := store.DeleteAdminUser(admin.ID); AsHTTPError(err).Code != "last_admin_user" {
		t.Fatalf("expected last admin deletion to be rejected, got %v", err)
	}
	if err := store.DeleteAdminUser(member.ID); err != nil {
		t.Fatalf("expected ordinary user deletion to remain allowed, got %v", err)
	}
}

func TestUpdateAdminUserProtectsLastActivePlatformAdmin(t *testing.T) {
	tests := []struct {
		name  string
		patch AdminUser
	}{
		{name: "disable", patch: AdminUser{Status: StatusDisabled}},
		{name: "demote", patch: AdminUser{Role: "user"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMemoryStore()
			admin := createTestAdminUser(t, store, "only-admin-"+tt.name, "system_admin")
			createTestAdminUser(t, store, "member-"+tt.name, "user")

			if _, err := store.UpdateAdminUser(admin.ID, tt.patch, ""); AsHTTPError(err).Code != "last_admin_user" {
				t.Fatalf("expected last admin update to be rejected, got %v", err)
			}
		})
	}
}

func TestAdminUserChangesAllowedWhenAnotherAdminRemains(t *testing.T) {
	store := NewMemoryStore()
	first := createTestAdminUser(t, store, "first-admin", "admin")
	createTestAdminUser(t, store, "second-admin", "system_admin")

	updated, err := store.UpdateAdminUser(first.ID, AdminUser{Role: "user"}, "")
	if err != nil {
		t.Fatalf("expected demotion with another active admin to succeed, got %v", err)
	}
	if updated.Role != "user" {
		t.Fatalf("expected demoted user role, got %q", updated.Role)
	}
}

func createTestAdminUser(t *testing.T, store *GormStore, username string, role string) AdminUser {
	t.Helper()
	user, err := store.CreateAdminUser(AdminUser{
		Username: username,
		Email:    username + "@example.com",
		Role:     role,
		Status:   StatusActive,
	}, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	return user
}
