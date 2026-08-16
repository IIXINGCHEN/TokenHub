package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminPasswordResetLinkKeepsTokenOutOfQueryAndUntrustedHosts(t *testing.T) {
	server := NewWithConfig(NewMemoryStore(), Config{
		PublicBaseURL:      "https://api.tokenhub.example",
		CORSAllowedOrigins: []string{"https://console.tokenhub.example"},
	})
	t.Cleanup(func() { _ = server.Shutdown(t.Context()) })
	request := httptest.NewRequest(http.MethodPost, "https://api.tokenhub.example/api/admin/users/user/reset-password-email", nil)
	request.RemoteAddr = "198.51.100.7:4321"
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Referer", "https://attacker.example/reset")
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	request.Header.Set("X-Forwarded-Proto", "http")

	const token = "password-reset-secret-token"
	link := server.adminPasswordResetLink(request, token)
	target, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if target.Scheme != "https" || target.Host != "console.tokenhub.example" || target.Path != "/" {
		t.Fatalf("password reset target trusted request headers: %s", link)
	}
	if target.Query().Has("reset_token") || strings.Contains(target.RawQuery, token) {
		t.Fatalf("password reset token leaked in query: %s", link)
	}
	fragment, err := url.ParseQuery(target.Fragment)
	if err != nil || fragment.Get("reset_token") != token {
		t.Fatalf("password reset token fragment = %q, err=%v", target.Fragment, err)
	}
}

func TestAdminPasswordResetTokenIsConsumedOnceAcrossInstances(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "password-reset.db")
	config := Config{SecretKey: "multi-instance-password-reset-secret"}
	storeA, err := NewSQLiteStoreWithConfig(databaseURL, config)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStoreWithConfig(databaseURL, config)
	if err != nil {
		t.Fatal(err)
	}
	for _, store := range []*GormStore{storeA, storeB} {
		sqlDB, dbErr := store.db.DB()
		if dbErr != nil {
			t.Fatal(dbErr)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	user, err := storeA.CreateAdminUser(AdminUser{
		Username: "reset-race-user", Email: "reset-race@example.test", Role: "user", Status: StatusActive,
	}, "old-password-123")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := storeA.CreateAdminPasswordResetToken(user.ID, user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	type resetResult struct {
		password string
		err      error
	}
	start := make(chan struct{})
	results := make(chan resetResult, 2)
	for index, store := range []*GormStore{storeA, storeB} {
		password := []string{"new-password-123-a", "new-password-123-b"}[index]
		go func(store *GormStore, password string) {
			<-start
			_, resetErr := store.ResetAdminUserPassword(token, password)
			results <- resetResult{password: password, err: resetErr}
		}(store, password)
	}
	close(start)
	successes := 0
	invalidTokens := 0
	successfulPassword := ""
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			successfulPassword = result.password
		case AsHTTPError(result.err).Code == "invalid_reset_token":
			invalidTokens++
		default:
			t.Fatalf("concurrent reset returned unexpected error: %v", result.err)
		}
	}
	if successes != 1 || invalidTokens != 1 {
		t.Fatalf("concurrent reset results: successes=%d invalid_tokens=%d", successes, invalidTokens)
	}
	if _, _, err := storeA.AuthenticateAdminUser(user.Email, successfulPassword, time.Hour); err != nil {
		t.Fatalf("winning password did not authenticate: %v", err)
	}
	for _, store := range []*GormStore{storeA, storeB} {
		if _, err := store.ResetAdminUserPassword(token, "replayed-password-123"); AsHTTPError(err).Code != "invalid_reset_token" {
			t.Fatalf("consumed reset token was reusable: %v", err)
		}
	}
}
