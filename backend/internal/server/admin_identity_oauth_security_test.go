package server

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type adminOAuthTestSession struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      AdminUser `json:"user"`
}

const testAdminOAuthCodeVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func testAdminOAuthCodeChallenge(t *testing.T) string {
	t.Helper()
	challenge, ok := adminOAuthCodeChallenge(testAdminOAuthCodeVerifier)
	if !ok {
		t.Fatal("test OAuth code verifier is invalid")
	}
	return challenge
}

func adminOAuthStartURLForTest(t *testing.T, rawURL string) string {
	t.Helper()
	target, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	query := target.Query()
	query.Set("code_challenge", testAdminOAuthCodeChallenge(t))
	query.Set("code_challenge_method", "S256")
	target.RawQuery = query.Encode()
	return target.String()
}

func requireResponseCookieWithPrefix(t *testing.T, response *httptest.ResponseRecorder, prefix string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if strings.HasPrefix(cookie.Name, prefix) && cookie.Value != "" && cookie.MaxAge >= 0 {
			return cookie
		}
	}
	t.Fatalf("response did not set cookie with prefix %q: %v", prefix, response.Header().Values("Set-Cookie"))
	return nil
}

func exchangeAdminOAuthCodeForTest(t *testing.T, handler http.Handler, code string, codeVerifier string) adminOAuthTestSession {
	t.Helper()
	body, err := json.Marshal(map[string]string{"code": code, "code_verifier": codeVerifier})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/auth/oauth/exchange", bytes.NewReader(body))
	request.Header.Set("content-type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("OAuth code exchange failed: status=%d body=%s", response.Code, response.Body.String())
	}
	var session adminOAuthTestSession
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Token == "" || session.User.ID == "" || session.ExpiresAt.IsZero() {
		t.Fatalf("incomplete OAuth session response: %s", response.Body.String())
	}
	return session
}

func TestIdentityProviderMutationsRequirePlatformAdmin(t *testing.T) {
	store := NewMemoryStore()
	_, err := store.CreateAdminUser(AdminUser{
		Username: "platform-admin", Email: "platform-admin@example.test", Role: "admin", Status: StatusActive,
	}, "platform-password")
	if err != nil {
		t.Fatal(err)
	}
	securityUser, err := store.CreateAdminUser(AdminUser{
		Username: "security-admin", Email: "security-admin@example.test", Role: "security_admin", Status: StatusActive,
	}, "security-password")
	if err != nil {
		t.Fatal(err)
	}
	_, securitySession, err := store.CreateAdminSession(securityUser.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	provider := store.CreateResource("identity-providers", AdminResource{
		ID: "idp_existing", Name: "Existing IdP", Status: StatusActive,
	})
	app := NewWithConfig(store, Config{AdminToken: "platform-token"}).Handler()

	read := doJSON(t, app, http.MethodGet, "/api/admin/resources/identity-providers", nil, securitySession.Token)
	if read.Code != http.StatusOK || !strings.Contains(read.Body, provider.ID) {
		t.Fatalf("security admin should retain IdP read access: status=%d body=%s", read.Code, read.Body)
	}
	mutations := []struct {
		method  string
		path    string
		payload any
	}{
		{http.MethodPost, "/api/admin/resources/identity-providers", map[string]any{"name": "Malicious IdP"}},
		{http.MethodPatch, "/api/admin/resources/identity-providers/" + provider.ID, map[string]any{"name": "Hijacked IdP"}},
		{http.MethodDelete, "/api/admin/resources/identity-providers/" + provider.ID, nil},
	}
	for _, mutation := range mutations {
		response := doJSON(t, app, mutation.method, mutation.path, mutation.payload, securitySession.Token)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body, "admin_forbidden") {
			t.Fatalf("security admin %s should be forbidden: status=%d body=%s", mutation.method, response.Code, response.Body)
		}
	}
	created := doJSON(t, app, http.MethodPost, "/api/admin/resources/identity-providers", map[string]any{"name": "Platform IdP"}, "platform-token")
	if created.Code != http.StatusCreated {
		t.Fatalf("platform admin should manage IdPs: status=%d body=%s", created.Code, created.Body)
	}
}

func TestSafeOAuthReturnURLIgnoresOriginAndReferer(t *testing.T) {
	server := NewWithConfig(NewMemoryStore(), Config{
		PublicBaseURL:      "https://api.tokenhub.example",
		CORSAllowedOrigins: []string{"not-an-origin", "https://console.tokenhub.example:8443"},
	})
	request := httptest.NewRequest(http.MethodGet, "http://internal:8080/api/admin/auth/oauth/start", nil)
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Referer", "https://attacker.example/login")

	if got := server.safeOAuthReturnURL("https://attacker.example/steal", request); got != "https://console.tokenhub.example:8443/overview" {
		t.Fatalf("untrusted return URL fell back to %q", got)
	}
	if got := server.safeOAuthReturnURL("", request); got != "https://console.tokenhub.example:8443/overview" {
		t.Fatalf("empty return URL trusted request headers: %q", got)
	}
	if got := server.safeOAuthReturnURL("https://console.tokenhub.example:8443/settings?tab=sso", request); got != "https://console.tokenhub.example:8443/settings?tab=sso" {
		t.Fatalf("configured CORS origin should be allowed: %q", got)
	}
	if got := server.safeOAuthReturnURL("https://api.tokenhub.example/settings", request); got != "https://api.tokenhub.example/settings" {
		t.Fatalf("public base origin should be allowed: %q", got)
	}
	if got := server.safeOAuthReturnURL("https://api.tokenhub.example/overview?language=zh&oauth_token=attacker#oauth_code=stale", request); got != "https://api.tokenhub.example/overview?language=zh" {
		t.Fatalf("reserved OAuth parameters were retained: %q", got)
	}
	for _, candidate := range []string{
		"https://console.tokenhub.example/settings",
		"http://console.tokenhub.example:8443/settings",
		"https://console.tokenhub.example:9443/settings",
		"http://localhost:3000/overview",
	} {
		if got := server.safeOAuthReturnURL(candidate, request); got != "https://console.tokenhub.example:8443/overview" {
			t.Fatalf("return URL %q bypassed exact-origin validation: %q", candidate, got)
		}
	}

	request.Host = "admin.internal.test:8080"
	server.config = Config{}
	if got := server.safeOAuthReturnURL("https://attacker.example/steal", request); got != "http://admin.internal.test:8080/overview" {
		t.Fatalf("request Host fallback = %q", got)
	}

	loopbackRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/admin/auth/oauth/start", nil)
	if got := server.safeOAuthReturnURL("http://localhost:3000/overview", loopbackRequest); got != "http://localhost:3000/overview" {
		t.Fatalf("loopback development return URL should remain compatible: %q", got)
	}
}

func TestAdminOAuthStartRequiresS256PKCE(t *testing.T) {
	store := NewMemoryStore()
	store.CreateResource("identity-providers", AdminResource{
		ID: "idp_pkce", Name: "PKCE IdP", Status: StatusActive,
		Fields: map[string]any{
			"provider_type": "oauth2", "client_id": "client", "authorize_url": "https://idp.example/authorize",
			"token_url": "https://idp.example/token", "userinfo_url": "https://idp.example/userinfo",
			"redirect_uri": "https://tokenhub.example/api/admin/auth/oauth/callback",
		},
	})
	app := New(store).Handler()

	for _, rawURL := range []string{
		"/api/admin/auth/oauth/start?id=idp_pkce",
		"/api/admin/auth/oauth/start?id=idp_pkce&code_challenge=invalid&code_challenge_method=S256",
		"/api/admin/auth/oauth/start?id=idp_pkce&code_challenge=" + url.QueryEscape(testAdminOAuthCodeChallenge(t)) + "&code_challenge_method=plain",
	} {
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest(http.MethodGet, rawURL, nil))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_oauth_code_challenge") {
			t.Fatalf("OAuth start without valid PKCE = %d: %s", response.Code, response.Body.String())
		}
	}

	validResponse := httptest.NewRecorder()
	app.ServeHTTP(validResponse, httptest.NewRequest(http.MethodGet, adminOAuthStartURLForTest(t, "/api/admin/auth/oauth/start?id=idp_pkce"), nil))
	if validResponse.Code != http.StatusFound {
		t.Fatalf("OAuth start with S256 PKCE = %d: %s", validResponse.Code, validResponse.Body.String())
	}
}

func TestOAuthCallbackURLUsesOnlyConfiguredOrTrustedOrigins(t *testing.T) {
	untrusted := NewWithConfig(NewMemoryStore(), Config{})
	request := httptest.NewRequest(http.MethodGet, "https://api.tokenhub.example/api/admin/auth/oauth/start", nil)
	request.RemoteAddr = "198.51.100.7:4321"
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	got, err := untrusted.oauthCallbackURL(request)
	if err != nil || got != "https://api.tokenhub.example/api/admin/auth/oauth/callback" {
		t.Fatalf("untrusted forwarded headers changed callback: callback=%q err=%v", got, err)
	}

	configured := NewWithConfig(NewMemoryStore(), Config{PublicBaseURL: "https://public.tokenhub.example/base"})
	got, err = configured.oauthCallbackURL(request)
	if err != nil || got != "https://public.tokenhub.example/api/admin/auth/oauth/callback" {
		t.Fatalf("public base callback = %q, err=%v", got, err)
	}

	trusted := NewWithConfig(NewMemoryStore(), Config{TrustedProxyCIDRs: []string{"10.0.0.0/8"}})
	trustedRequest := httptest.NewRequest(http.MethodGet, "http://tokenhub-backend:8080/api/admin/auth/oauth/start", nil)
	trustedRequest.RemoteAddr = "10.0.0.8:4321"
	trustedRequest.Header.Set("X-Forwarded-Proto", "https")
	trustedRequest.Header.Set("X-Forwarded-Host", "gateway.tokenhub.example")
	got, err = trusted.oauthCallbackURL(trustedRequest)
	if err != nil || got != "https://gateway.tokenhub.example/api/admin/auth/oauth/callback" {
		t.Fatalf("trusted proxy callback = %q, err=%v", got, err)
	}
}

func TestIdentityProviderRedirectURIRejectsInsecureExternalHTTP(t *testing.T) {
	server := New(NewMemoryStore())
	request := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/admin/auth/oauth/start", nil)
	provider := AdminResource{Fields: map[string]any{"redirect_uri": "http://oauth.example/api/admin/auth/oauth/callback"}}
	if _, err := server.identityProviderRedirectURI(provider, request); AsHTTPError(err).Code != "insecure_redirect_uri" {
		t.Fatalf("external HTTP callback error = %v", err)
	}
	provider.Fields["redirect_uri"] = "http://localhost:8080/api/admin/auth/oauth/callback"
	if got, err := server.identityProviderRedirectURI(provider, request); err != nil || got != provider.Fields["redirect_uri"] {
		t.Fatalf("loopback callback = %q, err=%v", got, err)
	}
}

func TestAdminOAuthStateCookieIsBrowserBoundAndSingleUse(t *testing.T) {
	store := NewMemoryStore()
	store.CreateResource("identity-providers", AdminResource{
		ID: "idp_browser_bound", Name: "Browser-bound IdP", Status: StatusActive,
		Fields: map[string]any{
			"provider_type": "oauth2", "client_id": "client", "authorize_url": "https://idp.example/authorize",
			"token_url": "https://idp.example/token", "userinfo_url": "https://idp.example/userinfo",
			"redirect_uri": "https://tokenhub.example/api/admin/auth/oauth/callback",
		},
	})
	app := NewWithConfig(store, Config{PublicBaseURL: "https://tokenhub.example", SecretKey: "test-secret"}).Handler()
	startRequest := httptest.NewRequest(http.MethodGet, adminOAuthStartURLForTest(t, "https://tokenhub.example/api/admin/auth/oauth/start?id=idp_browser_bound"), nil)
	startResponse := httptest.NewRecorder()
	app.ServeHTTP(startResponse, startRequest)
	if startResponse.Code != http.StatusFound {
		t.Fatalf("OAuth start failed: status=%d body=%s", startResponse.Code, startResponse.Body.String())
	}
	stateCookie := requireResponseCookieWithPrefix(t, startResponse, adminOAuthStateCookiePrefix)
	if !stateCookie.HttpOnly || !stateCookie.Secure || stateCookie.SameSite != http.SameSiteLaxMode || stateCookie.Path != adminOAuthStateCookiePath {
		t.Fatalf("unsafe OAuth state cookie: %+v", stateCookie)
	}
	authorizeURL, err := url.Parse(startResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := authorizeURL.Query().Get("state")
	callbackURL := "https://tokenhub.example/api/admin/auth/oauth/callback?error=access_denied&state=" + url.QueryEscape(state)

	withoutCookie := httptest.NewRecorder()
	app.ServeHTTP(withoutCookie, httptest.NewRequest(http.MethodGet, callbackURL, nil))
	if withoutCookie.Code != http.StatusBadRequest || !strings.Contains(withoutCookie.Body.String(), "invalid_oauth_state") {
		t.Fatalf("callback without browser cookie should fail: status=%d body=%s", withoutCookie.Code, withoutCookie.Body.String())
	}

	callbackRequest := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	callbackRequest.AddCookie(stateCookie)
	callbackResponse := httptest.NewRecorder()
	app.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusFound {
		t.Fatalf("bound callback failed: status=%d body=%s", callbackResponse.Code, callbackResponse.Body.String())
	}
	location, err := url.Parse(callbackResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Query().Has("oauth_error") || !strings.Contains(location.Fragment, "oauth_error=provider_error") {
		t.Fatalf("OAuth error must only be returned in fragment: %s", location.String())
	}

	replayRequest := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	replayRequest.AddCookie(stateCookie)
	replayResponse := httptest.NewRecorder()
	app.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusBadRequest || !strings.Contains(replayResponse.Body.String(), "invalid_oauth_state") {
		t.Fatalf("consumed OAuth state was reusable: status=%d body=%s", replayResponse.Code, replayResponse.Body.String())
	}
}

func TestAdminOAuthExchangeIsPKCEBoundAndSingleUse(t *testing.T) {
	store := NewMemoryStore()
	user, err := store.CreateAdminUser(AdminUser{
		Username: "oauth-user", Email: "oauth-user@example.test", Role: "user", Status: StatusActive,
	}, "oauth-password")
	if err != nil {
		t.Fatal(err)
	}
	code := "single-use-exchange-code"
	challenge := testAdminOAuthCodeChallenge(t)
	if err := store.SaveAdminOAuthExchange(adminOAuthExchange{Code: code, CodeChallenge: challenge, UserID: user.ID}); err != nil {
		t.Fatal(err)
	}
	app := New(store).Handler()

	requestBody := []byte(`{"code":"single-use-exchange-code","code_verifier":"wrong-verifier-that-is-still-long-enough-1234567890"}`)
	wrongVerifier := httptest.NewRequest(http.MethodPost, "/api/admin/auth/oauth/exchange", bytes.NewReader(requestBody))
	wrongVerifier.Header.Set("content-type", "application/json")
	wrongVerifierResponse := httptest.NewRecorder()
	app.ServeHTTP(wrongVerifierResponse, wrongVerifier)
	if wrongVerifierResponse.Code != http.StatusBadRequest {
		t.Fatalf("exchange with the wrong PKCE verifier should fail: %d", wrongVerifierResponse.Code)
	}

	session := exchangeAdminOAuthCodeForTest(t, app, code, testAdminOAuthCodeVerifier)
	if session.User.ID != user.ID {
		t.Fatalf("exchange returned wrong user: %+v", session.User)
	}
	replayBody := []byte(`{"code":"single-use-exchange-code","code_verifier":"` + testAdminOAuthCodeVerifier + `"}`)
	replay := httptest.NewRequest(http.MethodPost, "/api/admin/auth/oauth/exchange", bytes.NewReader(replayBody))
	replay.Header.Set("content-type", "application/json")
	replayResponse := httptest.NewRecorder()
	app.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusBadRequest || !strings.Contains(replayResponse.Body.String(), "invalid_oauth_code") {
		t.Fatalf("consumed OAuth code was reusable: status=%d body=%s", replayResponse.Code, replayResponse.Body.String())
	}
}

func TestAdminOAuthFlowPersistsAcrossInstances(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "oauth-flow.db")
	config := Config{SecretKey: "shared-oauth-secret"}
	storeA, err := NewSQLiteStoreWithConfig(databaseURL, config)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStoreWithConfig(databaseURL, config)
	if err != nil {
		t.Fatal(err)
	}
	flow := adminOAuthFlow{
		State: "cross-instance-state", BrowserNonce: "cross-instance-browser", ProviderID: "idp_shared",
		ReturnURL: "https://tokenhub.example/overview", RedirectURI: "https://tokenhub.example/api/admin/auth/oauth/callback",
		CodeChallenge: testAdminOAuthCodeChallenge(t),
	}
	if err := storeA.SaveAdminOAuthFlow(flow); err != nil {
		t.Fatal(err)
	}
	consumed, ok, err := storeB.ConsumeAdminOAuthFlow(flow.State, flow.BrowserNonce)
	if err != nil || !ok || consumed.ProviderID != flow.ProviderID {
		t.Fatalf("second instance did not consume OAuth flow: flow=%+v ok=%v err=%v", consumed, ok, err)
	}
	if _, ok, err := storeA.ConsumeAdminOAuthFlow(flow.State, flow.BrowserNonce); err != nil || ok {
		t.Fatalf("first instance replay result: ok=%v err=%v", ok, err)
	}
	exchange := adminOAuthExchange{Code: "cross-instance-code", CodeChallenge: flow.CodeChallenge, UserID: "usr_shared"}
	if err := storeA.SaveAdminOAuthExchange(exchange); err != nil {
		t.Fatal(err)
	}
	if consumed, ok, err := storeB.ConsumeAdminOAuthExchange(exchange.Code, testAdminOAuthCodeVerifier); err != nil || !ok || consumed.UserID != exchange.UserID {
		t.Fatalf("second instance did not consume OAuth exchange: exchange=%+v ok=%v err=%v", consumed, ok, err)
	}
}

func TestOAuthRedirectValuesAreFragmentOnly(t *testing.T) {
	redirect := oauthRedirectWithFragment("https://tokenhub.example/overview?language=zh#old", url.Values{"oauth_code": {"short-code"}})
	target, err := url.Parse(redirect)
	if err != nil {
		t.Fatal(err)
	}
	if target.Query().Get("language") != "zh" || target.Query().Has("oauth_code") {
		t.Fatalf("redirect query was changed or contains OAuth data: %s", redirect)
	}
	if target.Fragment != "oauth_code=short-code" {
		t.Fatalf("redirect fragment = %q", target.Fragment)
	}
}

func TestSecureCookieDetectionUsesCallbackScheme(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://tokenhub.example/api/admin/auth/oauth/start", nil)
	if request.TLS == nil {
		request.TLS = &tls.ConnectionState{}
	}
	if got := canonicalOAuthReturnURL(Config{}, request); got != "https://tokenhub.example/overview" {
		t.Fatalf("TLS request fallback = %q", got)
	}
}
