package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestParseAndCompareSemanticVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{name: "patch", left: "0.3.1", right: "0.3.0", want: 1},
		{name: "minor", left: "0.10.0", right: "0.9.9", want: 1},
		{name: "v prefix", left: "v1.2.3", right: "1.2.3", want: 0},
		{name: "prerelease before stable", left: "1.0.0-rc.1", right: "1.0.0", want: -1},
		{name: "numeric prerelease order", left: "1.0.0-rc.10", right: "1.0.0-rc.2", want: 1},
		{name: "numeric before text", left: "1.0.0-1", right: "1.0.0-alpha", want: -1},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, left, leftOK := parseSemanticVersion(test.left)
			_, right, rightOK := parseSemanticVersion(test.right)
			if !leftOK || !rightOK {
				t.Fatalf("failed to parse versions %q and %q", test.left, test.right)
			}
			if got := compareSemanticVersions(left, right); got != test.want {
				t.Fatalf("compareSemanticVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestParseSemanticVersionRejectsInvalidValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "latest", "1", "1.2", "01.2.3", "1.2.3-", "1.2.3-01", "1.2.3-a_b", "1.2.3+", "1.2.3+bad_build"} {
		if _, _, ok := parseSemanticVersion(value); ok {
			t.Fatalf("parseSemanticVersion(%q) unexpectedly succeeded", value)
		}
	}
}

func TestVersionServiceUsesConfiguredReleaseRepository(t *testing.T) {
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/wangle201210/TokenHub/releases/latest" {
			http.Error(w, "unexpected release path: "+r.URL.String(), http.StatusBadRequest)
			return
		}
		writeTestJSON(w, map[string]any{"tag_name": "v0.3.1"})
	}))
	defer releases.Close()

	service := newVersionService(Config{
		AppVersion:        "0.3.0",
		BuildType:         defaultBuildType,
		ReleaseRepository: "wangle201210/TokenHub",
	})
	service.apiBaseURL = releases.URL
	service.client = releases.Client()

	info := service.checkUpdate(t.Context(), true)
	if !info.HasUpdate || info.ReleasesURL != "https://github.com/wangle201210/TokenHub/releases" {
		t.Fatalf("unexpected configured repository response: %+v", info)
	}
	if info.ReleaseInfo == nil || info.ReleaseInfo.HTMLURL != "https://github.com/wangle201210/TokenHub/releases/tag/v0.3.1" {
		t.Fatalf("unexpected configured release link: %+v", info.ReleaseInfo)
	}
}

func TestValidateForStartupRejectsInvalidReleaseRepository(t *testing.T) {
	config := Config{Environment: "dev", ReleaseRepository: "owner/repo/extra"}
	if err := config.ValidateForStartup(); err == nil {
		t.Fatal("ValidateForStartup accepted an invalid release repository")
	}
}

func TestValidateForStartupRejectsInvalidNativeInstallRoot(t *testing.T) {
	config := Config{
		Environment:    "dev",
		BuildType:      releaseBuildType,
		DeploymentType: nativeDeploymentType,
		InstallRoot:    "relative/path",
	}
	if err := config.ValidateForStartup(); err == nil {
		t.Fatal("ValidateForStartup accepted a relative native install root")
	}
}

func TestVersionServiceChecksLatestReleaseAndCachesResult(t *testing.T) {
	var calls atomic.Int32
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/repos/astaxie/TokenHub/releases/latest" {
			http.Error(w, "unexpected release path: "+r.URL.String(), http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("User-Agent"); got != "TokenHub/0.3.0" {
			http.Error(w, "unexpected user agent: "+got, http.StatusBadRequest)
			return
		}
		writeTestJSON(w, map[string]any{
			"tag_name":     "v0.4.0",
			"name":         "TokenHub 0.4.0",
			"published_at": "2026-07-24T08:00:00Z",
		})
	}))
	defer releases.Close()

	service := testVersionService(releases, "0.3.0", releaseBuildType)
	info := service.checkUpdate(t.Context(), false)
	if info.CurrentVersion != "0.3.0" || info.LatestVersion != "0.4.0" || !info.HasUpdate {
		t.Fatalf("unexpected version info: %+v", info)
	}
	if info.BuildType != releaseBuildType || info.DeploymentType != containerDeploymentType || info.ReleaseInfo == nil {
		t.Fatalf("missing release metadata: %+v", info)
	}
	if info.ReleaseInfo.HTMLURL != "https://github.com/astaxie/TokenHub/releases/tag/v0.4.0" {
		t.Fatalf("unexpected release URL: %q", info.ReleaseInfo.HTMLURL)
	}

	cached := service.checkUpdate(t.Context(), false)
	if !cached.Cached || calls.Load() != 1 {
		t.Fatalf("expected a cached response after one upstream call: cached=%v calls=%d", cached.Cached, calls.Load())
	}
	_ = service.checkUpdate(t.Context(), true)
	if calls.Load() != 2 {
		t.Fatalf("forced refresh made %d upstream calls, want 2", calls.Load())
	}
}

func TestVersionServiceHandlesRepositoryWithoutReleases(t *testing.T) {
	var calls atomic.Int32
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer releases.Close()

	service := testVersionService(releases, "0.3.0", defaultBuildType)
	info := service.checkUpdate(t.Context(), false)
	if info.HasUpdate || info.Warning != "" || info.LatestVersion != "0.3.0" {
		t.Fatalf("unexpected no-release response: %+v", info)
	}
	cached := service.checkUpdate(t.Context(), false)
	if !cached.Cached || calls.Load() != 1 {
		t.Fatalf("expected no-release response to be cached: cached=%v calls=%d", cached.Cached, calls.Load())
	}
}

func TestVersionServiceCachesEmptyRollbackHistory(t *testing.T) {
	var calls atomic.Int32
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer releases.Close()

	service := testVersionService(releases, "0.3.0", defaultBuildType)
	for attempt := 0; attempt < 2; attempt++ {
		versions, err := service.listRollbackVersions(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(versions) != 0 {
			t.Fatalf("rollback versions = %v, want none", versions)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("empty rollback history made %d upstream calls, want 1", calls.Load())
	}
}

func TestVersionServiceListsThreeNewestOlderStableReleases(t *testing.T) {
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/astaxie/TokenHub/releases" || r.URL.Query().Get("per_page") != "15" {
			http.Error(w, "unexpected release history path: "+r.URL.String(), http.StatusBadRequest)
			return
		}
		writeTestJSON(w, []map[string]any{
			{"tag_name": "v0.2.8", "published_at": "2026-07-18T08:00:00Z"},
			{"tag_name": "v0.4.0", "published_at": "2026-07-24T08:00:00Z"},
			{"tag_name": "v0.2.10", "published_at": "2026-07-22T08:00:00Z"},
			{"tag_name": "v0.3.0", "published_at": "2026-07-23T08:00:00Z"},
			{"tag_name": "v0.2.9", "published_at": "2026-07-20T08:00:00Z"},
			{"tag_name": "v0.2.7", "published_at": "2026-07-16T08:00:00Z"},
			{"tag_name": "v0.2.11-rc.1", "prerelease": true},
			{"tag_name": "v0.2.12", "draft": true},
			{"tag_name": "not-semver"},
		})
	}))
	defer releases.Close()

	versions, err := testVersionService(releases, "0.3.0", releaseBuildType).listRollbackVersions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(versions))
	for _, version := range versions {
		got = append(got, version.Version)
	}
	want := []string{"0.2.10", "0.2.9", "0.2.8"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback versions = %v, want %v", got, want)
	}
}

func TestAdminVersionEndpointsRequireAuthenticationAndReturnReleaseData(t *testing.T) {
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/astaxie/TokenHub/releases/latest" {
			writeTestJSON(w, map[string]any{"tag_name": "v0.3.1", "name": "TokenHub 0.3.1"})
			return
		}
		writeTestJSON(w, []map[string]any{{"tag_name": "v0.2.9", "published_at": "2026-07-20T08:00:00Z"}})
	}))
	defer releases.Close()

	app := NewWithConfig(NewMemoryStore(), Config{
		AdminToken: "version-admin-token",
		AppVersion: "0.3.0",
		BuildType:  releaseBuildType,
	})
	app.versions = testVersionService(releases, "0.3.0", releaseBuildType)

	unauthorized := httptest.NewRecorder()
	app.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/admin/system/version", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated version status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	versionResponse := requestVersionEndpoint(t, app.Handler(), "/api/admin/system/version?force=true", "version-admin-token")
	var info systemVersionInfo
	if err := json.Unmarshal(versionResponse.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if versionResponse.Code != http.StatusOK || info.LatestVersion != "0.3.1" || !info.HasUpdate {
		t.Fatalf("unexpected version response: status=%d body=%s", versionResponse.Code, versionResponse.Body.String())
	}

	rollbackResponse := requestVersionEndpoint(t, app.Handler(), "/api/admin/system/rollback-versions", "version-admin-token")
	var payload struct {
		Versions []rollbackVersionInfo `json:"versions"`
	}
	if err := json.Unmarshal(rollbackResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if rollbackResponse.Code != http.StatusOK || len(payload.Versions) != 1 || payload.Versions[0].Version != "0.2.9" {
		t.Fatalf("unexpected rollback response: status=%d body=%s", rollbackResponse.Code, rollbackResponse.Body.String())
	}
}

func testVersionService(server *httptest.Server, currentVersion, buildType string) *versionService {
	service := newVersionService(Config{AppVersion: currentVersion, BuildType: buildType})
	service.apiBaseURL = server.URL
	service.client = server.Client()
	return service
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("content-type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(err)
	}
}

func requestVersionEndpoint(t *testing.T, handler http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
