package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeUpdateDownloadsVerifiesAndActivatesRelease(t *testing.T) {
	root := prepareNativeInstallRoot(t, "0.3.1", nil)
	archive := nativeTestArchive(t, "0.3.2", nil)
	checksum := sha256.Sum256(archive)
	assetName := "tokenhub_0.3.2_linux_amd64.tar.gz"

	var releases *httptest.Server
	releases = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/test/TokenHub/releases/latest":
			writeTestJSON(w, nativeTestRelease("v0.3.2", assetName, releases.URL))
		case "/assets/" + assetName:
			_, _ = w.Write(archive)
		case "/assets/checksums.txt":
			_, _ = w.Write([]byte(hex.EncodeToString(checksum[:]) + "  " + assetName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer releases.Close()

	service := nativeTestVersionService(root, "0.3.1", releases)
	version, err := service.performNativeUpdate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if version != "0.3.2" {
		t.Fatalf("updated version = %q, want 0.3.2", version)
	}
	assertNativeCurrentVersion(t, root, "0.3.2")
	if _, err := os.Stat(filepath.Join(root, "releases", "0.3.1", "VERSION")); err != nil {
		t.Fatalf("previous release was not preserved: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "current", "bin", "tokenhub"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "tokenhub-0.3.2" {
		t.Fatalf("activated backend content = %q", content)
	}
	version, err = service.performNativeUpdate(t.Context())
	if err != nil || version != "0.3.2" {
		t.Fatalf("repeated update = %q, %v; want idempotent 0.3.2", version, err)
	}
}

func TestNativeUpdateChecksumFailureKeepsCurrentRelease(t *testing.T) {
	root := prepareNativeInstallRoot(t, "0.3.1", nil)
	archive := nativeTestArchive(t, "0.3.2", nil)
	assetName := "tokenhub_0.3.2_linux_amd64.tar.gz"

	var releases *httptest.Server
	releases = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/test/TokenHub/releases/latest":
			writeTestJSON(w, nativeTestRelease("v0.3.2", assetName, releases.URL))
		case "/assets/" + assetName:
			_, _ = w.Write(archive)
		case "/assets/checksums.txt":
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + assetName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer releases.Close()

	service := nativeTestVersionService(root, "0.3.1", releases)
	if _, err := service.performNativeUpdate(t.Context()); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("performNativeUpdate error = %v, want checksum failure", err)
	}
	assertNativeCurrentVersion(t, root, "0.3.1")
	if _, err := os.Stat(filepath.Join(root, "releases", "0.3.2")); !os.IsNotExist(err) {
		t.Fatalf("failed update left an installed target: %v", err)
	}
}

func TestExtractNativeArchiveRejectsPathTraversal(t *testing.T) {
	archive := nativeTestArchive(t, "0.3.2", map[string]nativeTestArchiveEntry{
		"../escaped": {content: "unsafe", mode: 0644},
	})
	archivePath := filepath.Join(t.TempDir(), "release.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "bundle")
	err := extractNativeArchive(archivePath, destination)
	if err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("extractNativeArchive error = %v, want unsafe path rejection", err)
	}
}

func TestNativeRollbackActivatesInstalledAllowedRelease(t *testing.T) {
	root := prepareNativeInstallRoot(t, "0.3.2", map[string]string{"0.3.1": "old"})
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/test/TokenHub/releases" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, []map[string]any{
			{"tag_name": "v0.3.2"},
			{"tag_name": "v0.3.1"},
		})
	}))
	defer releases.Close()

	service := nativeTestVersionService(root, "0.3.2", releases)
	version, err := service.rollbackNativeRelease(context.Background(), "0.3.1")
	if err != nil {
		t.Fatal(err)
	}
	if version != "0.3.1" {
		t.Fatalf("rollback version = %q, want 0.3.1", version)
	}
	assertNativeCurrentVersion(t, root, "0.3.1")
	version, err = service.rollbackNativeRelease(context.Background(), "0.3.1")
	if err != nil || version != "0.3.1" {
		t.Fatalf("repeated rollback = %q, %v; want idempotent 0.3.1", version, err)
	}
}

func TestNativeVersionInfoReportsPendingRestartAfterActivation(t *testing.T) {
	root := prepareNativeInstallRoot(t, "0.3.1", map[string]string{"0.3.2": "next"})
	var calls int
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/test/TokenHub/releases/latest" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(w, map[string]any{"tag_name": "v0.3.2"})
	}))
	defer releases.Close()

	service := nativeTestVersionService(root, "0.3.1", releases)
	initial := service.checkUpdate(t.Context(), false)
	if initial.PendingRestart != "" {
		t.Fatalf("initial pending restart version = %q, want empty", initial.PendingRestart)
	}
	if err := service.activateNativeRelease("0.3.2"); err != nil {
		t.Fatal(err)
	}

	cached := service.checkUpdate(t.Context(), false)
	if cached.PendingRestart != "0.3.2" {
		t.Fatalf("cached pending restart version = %q, want 0.3.2", cached.PendingRestart)
	}
	if calls != 1 {
		t.Fatalf("cached pending restart check made %d GitHub calls, want 1", calls)
	}
}

func TestContainerDeploymentRejectsOnlineUpdateEndpoint(t *testing.T) {
	app := NewWithConfig(NewMemoryStore(), Config{
		AdminToken:     "native-test-admin-token",
		AppVersion:     "0.3.1",
		BuildType:      releaseBuildType,
		DeploymentType: containerDeploymentType,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/system/update", nil)
	request.Header.Set("authorization", "Bearer native-test-admin-token")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("container update status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "native_update_unavailable" {
		t.Fatalf("container update error code = %q", payload.Error.Code)
	}
}

func TestNativeRestartEndpointSignalsProcessAfterResponse(t *testing.T) {
	app := NewWithConfig(NewMemoryStore(), Config{
		AdminToken:     "native-test-admin-token",
		AppVersion:     "0.3.2",
		BuildType:      releaseBuildType,
		DeploymentType: nativeDeploymentType,
		InstallRoot:    t.TempDir(),
	})
	restarted := make(chan struct{})
	app.versions.restartProcess = func() error {
		close(restarted)
		return nil
	}

	request := httptest.NewRequest(http.MethodPost, "/api/admin/system/restart", nil)
	request.Header.Set("authorization", "Bearer native-test-admin-token")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("native restart status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("native restart callback was not invoked")
	}
}

func TestValidateNativeAssetURL(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"https://github.com/astaxie/TokenHub/releases/download/v0.3.2/archive.tar.gz",
		"https://release-assets.githubusercontent.com/github-production-release-asset/file",
	} {
		if err := validateNativeAssetURL(rawURL); err != nil {
			t.Errorf("validateNativeAssetURL(%q) = %v", rawURL, err)
		}
	}
	for _, rawURL := range []string{
		"http://github.com/astaxie/TokenHub/releases/download/v0.3.2/archive.tar.gz",
		"https://github.com:8443/archive.tar.gz",
		"https://example.com/archive.tar.gz",
	} {
		if err := validateNativeAssetURL(rawURL); err == nil {
			t.Errorf("validateNativeAssetURL(%q) unexpectedly succeeded", rawURL)
		}
	}
}

type nativeTestArchiveEntry struct {
	content string
	mode    int64
}

func nativeTestArchive(t *testing.T, version string, extra map[string]nativeTestArchiveEntry) []byte {
	t.Helper()
	entries := map[string]nativeTestArchiveEntry{
		"bin/tokenhub":               {content: "tokenhub-" + version, mode: 0755},
		"bin/node":                   {content: "node-" + version, mode: 0755},
		"bin/tokenhub-run":           {content: "run-" + version, mode: 0755},
		"frontend/server.js":         {content: "server-" + version, mode: 0644},
		"catalog/model-catalog.yaml": {content: "models: []", mode: 0644},
		"deploy/tokenhub.service":    {content: "[Service]", mode: 0644},
		"VERSION":                    {content: version + "\n", mode: 0644},
	}
	for name, entry := range extra {
		entries[name] = entry
	}

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, entry := range entries {
		header := &tar.Header{
			Name: name,
			Mode: entry.mode,
			Size: int64(len(entry.content)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func prepareNativeInstallRoot(t *testing.T, currentVersion string, additional map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "releases"), 0750); err != nil {
		t.Fatal(err)
	}
	createNativeTestBundle(t, filepath.Join(root, "releases", currentVersion), currentVersion)
	for version := range additional {
		createNativeTestBundle(t, filepath.Join(root, "releases", version), version)
	}
	if err := os.Symlink(filepath.Join("releases", currentVersion), filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	return root
}

func createNativeTestBundle(t *testing.T, root string, version string) {
	t.Helper()
	files := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"bin/tokenhub":               {content: "tokenhub-" + version, mode: 0755},
		"bin/node":                   {content: "node-" + version, mode: 0755},
		"bin/tokenhub-run":           {content: "run-" + version, mode: 0755},
		"frontend/server.js":         {content: "server-" + version, mode: 0644},
		"catalog/model-catalog.yaml": {content: "models: []", mode: 0644},
		"deploy/tokenhub.service":    {content: "[Service]", mode: 0644},
		"VERSION":                    {content: version + "\n", mode: 0644},
	}
	for name, file := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(file.content), file.mode); err != nil {
			t.Fatal(err)
		}
	}
}

func nativeTestRelease(tag string, assetName string, baseURL string) map[string]any {
	return map[string]any{
		"tag_name": tag,
		"assets": []map[string]any{
			{
				"name":                 assetName,
				"browser_download_url": baseURL + "/assets/" + assetName,
				"size":                 1024,
			},
			{
				"name":                 "checksums.txt",
				"browser_download_url": baseURL + "/assets/checksums.txt",
				"size":                 128,
			},
		},
	}
}

func nativeTestVersionService(root string, currentVersion string, releases *httptest.Server) *versionService {
	service := newVersionService(Config{
		AppVersion:        currentVersion,
		BuildType:         releaseBuildType,
		DeploymentType:    nativeDeploymentType,
		ReleaseRepository: "test/TokenHub",
		InstallRoot:       root,
	})
	service.apiBaseURL = releases.URL
	service.client = releases.Client()
	service.downloadClient = releases.Client()
	service.validateAssetURL = func(string) error { return nil }
	service.runtimeOS = "linux"
	service.runtimeArch = "amd64"
	return service
}

func assertNativeCurrentVersion(t *testing.T, root string, expected string) {
	t.Helper()
	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join("releases", expected) {
		t.Fatalf("current link = %q, want %q", target, filepath.Join("releases", expected))
	}
}
