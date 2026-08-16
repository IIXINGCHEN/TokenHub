package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
