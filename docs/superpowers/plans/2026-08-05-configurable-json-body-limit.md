# Configurable JSON Request Body Limits — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hard-coded 4 MiB `decodeJSON` limit with admin-configurable per-tier limits and a clear `413 Payload Too Large` response, unblocking large multimodal (Codex) requests.

**Architecture:** Add two `int64` config fields read from env (`getenvBytes` with size-suffix + hard ceiling). Convert `decodeJSON` to a `*Server` method using `http.MaxBytesReader` (streaming, typed 413). Migrate all ~37 uniform call sites; give multimodal chat endpoints the higher limit. Sync docs/env.

**Tech Stack:** Go 1.26, `net/http`, standard library only. Package `backend/internal/server`.

## Global Constraints

- Package under change: `backend/internal/server` (`config.go`, `http.go`).
- Env prefix `TOKENHUB_`; helpers live in `config.go` (`getenv`, `getenvInt`, `getenvBool`, `getenvList`).
- Defaults (verbatim): global `8 << 20` (8 MiB); multimodal `32 << 20` (32 MiB); hard ceiling `512 << 20` (512 MiB).
- Suffix parsing is binary (1k = 1024), case-insensitive: `b`, `k`/`kb`/`kib`, `m`/`mb`/`mib`, `g`/`gb`/`gib`.
- Error contract: over-limit → `NewHTTPError(413, "payload_too_large", …)`; other decode errors → `NewHTTPError(400, "invalid_request", err.Error())` (unchanged).
- `NewHTTPError(status int, code, message string) *HTTPError`; `AsHTTPError(err) *HTTPError` with fields `.Status`, `.Code`, `.Message`.
- Run from `backend/`: `gofmt -w <files>`, `go test ./...`, `go vet ./...` before handoff.
- Keep the OpenAI `/v1` contract unchanged for well-formed under-limit requests.

## File Structure

- `backend/internal/server/config.go` — add `MaxJSONRequestBytes`, `MaxMultimodalRequestBytes` fields, wire in `ConfigFromEnv`, add `getenvBytes` + `parseByteSize` + `maxConfigurableRequestBytes`.
- `backend/internal/server/config_test.go` — table tests for `getenvBytes`/`parseByteSize`.
- `backend/internal/server/http.go` — add `errors` import; add `(*Server).decodeJSON` + `(*Server).decodeJSONLimit`; migrate ~37 call sites; remove old free `decodeJSON`.
- `backend/internal/server/decode_test.go` — tests for the decode methods.
- `deploy/.env.example`, `start.sh`, `docs/deployment.md`, `docs/zh-CN/deployment.md`, `docs/ja/deployment.md` — env + docs sync.

---

### Task 1: Config fields + `getenvBytes` helper

**Files:**
- Modify: `backend/internal/server/config.go`
- Test: `backend/internal/server/config_test.go` (create)

**Interfaces:**
- Consumes: nothing (leaf).
- Produces: `Config.MaxJSONRequestBytes int64`, `Config.MaxMultimodalRequestBytes int64`; `getenvBytes(key string, fallback int64) int64`; const `maxConfigurableRequestBytes int64 = 512 << 20`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/server/config_test.go`:

```go
package server

import "testing"

func TestGetenvBytes(t *testing.T) {
	const fallback int64 = 8 << 20
	cases := []struct {
		name string
		set  bool
		val  string
		want int64
	}{
		{"unset", false, "", fallback},
		{"empty", true, "", fallback},
		{"raw", true, "1048576", 1 << 20},
		{"kib", true, "16k", 16 << 10},
		{"mb", true, "32mb", 32 << 20},
		{"mib_caps", true, "8MiB", 8 << 20},
		{"gib", true, "1g", 1 << 30},
		{"bytes_suffix", true, "2048b", 2048},
		{"garbage", true, "abc", fallback},
		{"zero", true, "0", fallback},
		{"negative", true, "-5", fallback},
		{"above_ceiling", true, "9999g", maxConfigurableRequestBytes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("TOKENHUB_TEST_BYTES", tc.val)
			} else {
				t.Setenv("TOKENHUB_TEST_BYTES", "")
			}
			if got := getenvBytes("TOKENHUB_TEST_BYTES", fallback); got != tc.want {
				t.Fatalf("getenvBytes(%q)=%d, want %d", tc.val, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/server/ -run TestGetenvBytes -v`
Expected: FAIL — `undefined: getenvBytes` / `undefined: maxConfigurableRequestBytes`.

- [ ] **Step 3: Add config fields, env wiring, and helper**

In `config.go`, add the two fields to `type Config struct` (after `ResourceCooldownSeconds int`):

```go
	MaxJSONRequestBytes       int64
	MaxMultimodalRequestBytes int64
```

In `ConfigFromEnv()`, add inside the returned `Config{...}` literal (after `ResourceCooldownSeconds: ...`):

```go
		MaxJSONRequestBytes:       getenvBytes("TOKENHUB_MAX_JSON_REQUEST_BYTES", 8<<20),
		MaxMultimodalRequestBytes: getenvBytes("TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES", 32<<20),
```

Add `"log"` and `"strconv"` to the import block. Append these definitions to `config.go`:

```go
const maxConfigurableRequestBytes int64 = 512 << 20

// getenvBytes reads a byte-size env var. It accepts a raw integer ("1048576")
// or a binary size suffix ("16k", "32mb", "8MiB", "1g"). Empty, unparseable,
// or non-positive values fall back. Values above maxConfigurableRequestBytes
// are clamped to the ceiling to guard against typos that would risk OOM.
func getenvBytes(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, ok := parseByteSize(raw)
	if !ok || value <= 0 {
		return fallback
	}
	if value > maxConfigurableRequestBytes {
		log.Printf("%s=%s exceeds the %d byte ceiling; clamping to ceiling", key, raw, maxConfigurableRequestBytes)
		return maxConfigurableRequestBytes
	}
	return value
}

func parseByteSize(raw string) (int64, bool) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(lower, "k"), strings.HasSuffix(lower, "kb"), strings.HasSuffix(lower, "kib"):
		multiplier = 1 << 10
	case strings.HasSuffix(lower, "m"), strings.HasSuffix(lower, "mb"), strings.HasSuffix(lower, "mib"):
		multiplier = 1 << 20
	case strings.HasSuffix(lower, "g"), strings.HasSuffix(lower, "gb"), strings.HasSuffix(lower, "gib"):
		multiplier = 1 << 30
	}
	digits := strings.TrimRight(lower, "kmgib")
	if digits == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	if multiplier > 1 && n > (int64(1)<<62)/multiplier {
		return 0, false // overflow
	}
	return n * multiplier, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && gofmt -w internal/server/config.go internal/server/config_test.go && go test ./internal/server/ -run TestGetenvBytes -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/server/config.go backend/internal/server/config_test.go
git commit -m "feat(config): add configurable JSON request body byte limits"
```

---

### Task 2: `decodeJSON`/`decodeJSONLimit` methods with 413

**Files:**
- Modify: `backend/internal/server/http.go` (add `errors` import; add methods near the existing free `decodeJSON` at ~line 6201 — do NOT remove the free function yet)
- Test: `backend/internal/server/decode_test.go` (create)

**Interfaces:**
- Consumes: `Config.MaxJSONRequestBytes` (Task 1); `NewHTTPError`, `AsHTTPError`.
- Produces: `(*Server).decodeJSON(w http.ResponseWriter, r *http.Request, target any) error`; `(*Server).decodeJSONLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) error`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/server/decode_test.go`:

```go
package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONLimitValid(t *testing.T) {
	s := &Server{}
	var out struct {
		Name string `json:"name"`
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ok"}`))
	w := httptest.NewRecorder()
	if err := s.decodeJSONLimit(w, r, &out, 1<<20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "ok" {
		t.Fatalf("got name=%q", out.Name)
	}
}

func TestDecodeJSONLimitTooLarge(t *testing.T) {
	s := &Server{}
	var out map[string]any
	body := `{"data":"` + strings.Repeat("a", 4096) + `"}`
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	w := httptest.NewRecorder()
	err := s.decodeJSONLimit(w, r, &out, 512)
	if err == nil {
		t.Fatal("expected error for over-limit body")
	}
	he := AsHTTPError(err)
	if he.Status != 413 || he.Code != "payload_too_large" {
		t.Fatalf("got status=%d code=%s, want 413/payload_too_large", he.Status, he.Code)
	}
}

func TestDecodeJSONLimitMalformed(t *testing.T) {
	s := &Server{}
	var out map[string]any
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"bad":`))
	w := httptest.NewRecorder()
	err := s.decodeJSONLimit(w, r, &out, 1<<20)
	if err == nil {
		t.Fatal("expected error for malformed body")
	}
	if he := AsHTTPError(err); he.Status != 400 {
		t.Fatalf("got status=%d, want 400", he.Status)
	}
}

func TestDecodeJSONUsesConfiguredDefault(t *testing.T) {
	s := &Server{config: Config{MaxJSONRequestBytes: 512}}
	var out map[string]any
	body := `{"data":"` + strings.Repeat("a", 4096) + `"}`
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	w := httptest.NewRecorder()
	err := s.decodeJSON(w, r, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if he := AsHTTPError(err); he.Status != 413 {
		t.Fatalf("got status=%d, want 413", he.Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/server/ -run TestDecodeJSON -v`
Expected: FAIL — `s.decodeJSONLimit undefined` (method not yet defined).

- [ ] **Step 3: Add `errors` import and the methods**

In `http.go`, add `"errors"` to the import block (alphabetically, before `"fmt"`).

Add these two methods immediately above the existing `func decodeJSON(r *http.Request, target any) error` (~line 6201). Leave the old free function in place for now so the package still builds:

```go
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return s.decodeJSONLimit(w, r, target, s.config.MaxJSONRequestBytes)
}

func (s *Server) decodeJSONLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return NewHTTPError(413, "payload_too_large", fmt.Sprintf("request body exceeds %d bytes", limit))
		}
		return NewHTTPError(400, "invalid_request", err.Error())
	}
	return nil
}
```

Note: a method `(*Server).decodeJSON` and the free function `decodeJSON` legally coexist (distinct because of the receiver). Both compile.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && gofmt -w internal/server/http.go internal/server/decode_test.go && go test ./internal/server/ -run TestDecodeJSON -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/server/http.go backend/internal/server/decode_test.go
git commit -m "feat(server): add MaxBytesReader decode methods returning 413"
```

---

### Task 3: Migrate call sites + remove old `decodeJSON`

**Files:**
- Modify: `backend/internal/server/http.go`, `backend/internal/server/provider_account_oauth.go` (all `decodeJSON(r, …)` call sites)

**Interfaces:**
- Consumes: `(*Server).decodeJSON`, `(*Server).decodeJSONLimit` (Task 2); `Config.MaxMultimodalRequestBytes` (Task 1).
- Produces: no new symbols; removes free `decodeJSON`.

- [ ] **Step 1: Rewrite all call sites to the method**

Every call site is `decodeJSON(r, &X)`. Rewrite to `s.decodeJSON(w, r, &X)`. The free-function *definition* is `decodeJSON(r *http.Request, …` (no comma after `r`), so a comma-anchored replace only touches calls:

```bash
cd backend/internal/server
perl -0pi -e 's/(?<![\w.])decodeJSON\(r, /s.decodeJSON(w, r, /g' http.go provider_account_oauth.go
```

- [ ] **Step 2: Collapse the now-redundant 400 wrap to pass the typed error through**

Only the wrap line immediately following one of our decode calls must change (so 413 propagates). Anchor on the decode call to avoid touching unrelated 400s:

```bash
cd backend/internal/server
perl -0pi -e 's/(s\.decodeJSON(?:Limit)?\([^\n]*\); err != nil \{\s*\n\s*)writeError\(w, r, NewHTTPError\(400, "invalid_request", err\.Error\(\)\)\)/${1}writeError(w, r, err)/g' http.go provider_account_oauth.go
```

- [ ] **Step 3: Give multimodal endpoints the higher limit**

In `http.go`, in `handleChatCompletions`, `handleResponses`, and `handleAdminPlaygroundChat`, change their `s.decodeJSON(w, r, &req)` line to:

```go
	if err := s.decodeJSONLimit(w, r, &req, s.config.MaxMultimodalRequestBytes); err != nil {
```

Locate them precisely:

```bash
cd backend/internal/server
grep -n "func (s \*Server) handleChatCompletions\|func (s \*Server) handleResponses\|func (s \*Server) handleAdminPlaygroundChat" http.go
```

Edit the `s.decodeJSON(w, r, &req)` on the first such line inside each of those three functions. (`/v1/embeddings` stays on the default — do not change `handleEmbeddings`.)

- [ ] **Step 4: Remove the old free `decodeJSON`**

Delete the now-unused free function in `http.go`:

```go
func decodeJSON(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4<<20))
	decoder.UseNumber()
	return decoder.Decode(target)
}
```

- [ ] **Step 5: Verify migration completeness, then build + vet + test**

```bash
cd backend
# No bare (non-method) decodeJSON calls or the old 4<<20 literal should remain:
! grep -rn "[^.]decodeJSON(r," internal/server/ && echo "no bare calls OK"
! grep -rn "io.LimitReader(r.Body, 4<<20)" internal/server/ && echo "old limiter gone OK"
# Multimodal endpoints wired (expect 3 matches):
grep -c "decodeJSONLimit(w, r, &req, s.config.MaxMultimodalRequestBytes)" internal/server/http.go
gofmt -w internal/server/http.go internal/server/provider_account_oauth.go
go build ./... && go vet ./... && go test ./...
```

Expected: both `grep` guards print their OK line; multimodal count is `3`; build/vet clean; all tests pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/server/http.go backend/internal/server/provider_account_oauth.go
git commit -m "feat(server): route all JSON decoding through configurable limits, 413 on overflow"
```

---

### Task 4: Env + documentation sync

**Files:**
- Modify: `deploy/.env.example`, `start.sh`, `docs/deployment.md`, `docs/zh-CN/deployment.md`, `docs/ja/deployment.md`

**Interfaces:**
- Consumes: env var names/defaults from Task 1.
- Produces: none (docs/config only).

- [ ] **Step 1: `deploy/.env.example`**

After line `TOKENHUB_RESOURCE_COOLDOWN_SECONDS=300`, add:

```
TOKENHUB_MAX_JSON_REQUEST_BYTES=8m
TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES=32m
```

- [ ] **Step 2: `start.sh`**

In the defaults block (after the `TOKENHUB_SECRET_KEY=...` line, ~line 14), add:

```bash
TOKENHUB_MAX_JSON_REQUEST_BYTES="${TOKENHUB_MAX_JSON_REQUEST_BYTES:-8m}"
TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES="${TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES:-32m}"
```

In the backend `exec env \` passthrough block (after the `TOKENHUB_SECRET_KEY="$TOKENHUB_SECRET_KEY" \` line), add:

```bash
    TOKENHUB_MAX_JSON_REQUEST_BYTES="$TOKENHUB_MAX_JSON_REQUEST_BYTES" \
    TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES="$TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES" \
```

- [ ] **Step 3: `docs/deployment.md` (English)**

After the `TOKENHUB_RESOURCE_COOLDOWN_SECONDS` table row (line 116), add:

```
| `TOKENHUB_MAX_JSON_REQUEST_BYTES` | `8m` | Max JSON request body for standard endpoints; accepts a raw byte count or a size suffix (`8m`, `512k`) |
| `TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES` | `32m` | Max JSON request body for multimodal chat endpoints (`/v1/chat/completions`, `/v1/responses`, admin playground); over-limit requests return `413 Payload Too Large` |
```

Then add a short note below the Backend Environment Variables table:

```
> Values above 512 MiB are clamped. When running behind a reverse proxy, set
> `client_max_body_size` at least as large as `TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES`,
> otherwise the proxy rejects large multimodal requests before they reach TokenHub.
```

- [ ] **Step 4: `docs/zh-CN/deployment.md`**

After its `TOKENHUB_RESOURCE_COOLDOWN_SECONDS` row (line 116), add the equivalent rows and note in Simplified Chinese:

```
| `TOKENHUB_MAX_JSON_REQUEST_BYTES` | `8m` | 标准接口的 JSON 请求体上限；支持纯字节数或大小后缀（`8m`、`512k`） |
| `TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES` | `32m` | 多模态聊天接口（`/v1/chat/completions`、`/v1/responses`、管理台 Playground）的 JSON 请求体上限；超限返回 `413 Payload Too Large` |
```

```
> 超过 512 MiB 的值会被截断到上限。使用反向代理时，请将 `client_max_body_size`
> 至少设为与 `TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES` 相同，否则大的多模态请求会在
> 到达 TokenHub 之前被代理拒绝。
```

- [ ] **Step 5: `docs/ja/deployment.md`**

After its `TOKENHUB_RESOURCE_COOLDOWN_SECONDS` row (line 116), add the equivalent rows and note in Japanese:

```
| `TOKENHUB_MAX_JSON_REQUEST_BYTES` | `8m` | 標準エンドポイントの JSON リクエストボディ上限。生のバイト数またはサイズ接尾辞（`8m`、`512k`）を指定可能 |
| `TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES` | `32m` | マルチモーダルのチャットエンドポイント（`/v1/chat/completions`、`/v1/responses`、管理コンソールの Playground）の JSON リクエストボディ上限。超過時は `413 Payload Too Large` を返す |
```

```
> 512 MiB を超える値は上限に丸められます。リバースプロキシ経由の場合は
> `client_max_body_size` を `TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES` 以上に設定してください。
> そうしないと、大きなマルチモーダルリクエストが TokenHub に届く前にプロキシで拒否されます。
```

- [ ] **Step 6: Validate and commit**

```bash
cd /Users/lilin/dev/source_code/TokenHub
git diff --check
docker compose --env-file deploy/.env.example -f deploy/docker-compose.yml config >/dev/null && echo "compose OK" || echo "compose skipped (docker unavailable)"
git add deploy/.env.example start.sh docs/deployment.md docs/zh-CN/deployment.md docs/ja/deployment.md
git commit -m "docs: document configurable JSON request body limits and proxy alignment"
```

Expected: `git diff --check` clean; compose config renders (or is skipped if Docker is unavailable — report which).

---

## Self-Review

**Spec coverage:**
- Config fields + env vars + suffix parsing + 512 MiB ceiling → Task 1. ✅
- `MaxBytesReader` + 413/400 classification + streaming preserved → Task 2. ✅
- Migrate 37 sites + multimodal endpoints (chat/responses/playground) + embeddings stays default → Task 3. ✅
- Tests (`getenvBytes` + decode 413/400/valid) → Tasks 1 & 2. ✅
- Docs/env sync across EN/zh/ja + proxy note → Task 4. ✅

**Placeholder scan:** none — all code and commands are concrete.

**Type consistency:** `decodeJSON(w, r, target)` and `decodeJSONLimit(w, r, target, limit)` signatures identical across Tasks 2–3; config field names `MaxJSONRequestBytes` / `MaxMultimodalRequestBytes` and const `maxConfigurableRequestBytes` consistent across Tasks 1–4; error codes `payload_too_large` / `invalid_request` consistent.
