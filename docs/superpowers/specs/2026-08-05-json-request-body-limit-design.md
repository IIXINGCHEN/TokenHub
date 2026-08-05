# Configurable JSON request body limits

**Date:** 2026-08-05
**Issue:** astaxie/TokenHub#125 — JSON 请求体 4 MiB 硬编码限制导致 Codex 多模态请求失败

## Problem

`decodeJSON` in `backend/internal/server/http.go` caps every JSON request body at a
hard-coded 4 MiB via `io.LimitReader(r.Body, 4<<20)`. It is used by ~37 handlers,
including the OpenAI-compatible endpoints `/v1/chat/completions`, `/v1/responses`,
and `/v1/embeddings`.

Multimodal requests (Codex, vision) inline base64-encoded images or large structured
context. Base64 adds ~33% overhead, so common image inputs exceed 4 MiB and fail.

Two defects in the current behavior:

1. **No configuration.** The 4 MiB ceiling cannot be changed without a code patch.
2. **Wrong failure mode.** `io.LimitReader` does not error on excess — it *silently
   truncates* at 4 MiB. The JSON decoder then hits EOF mid-stream and returns a
   generic parse error, which handlers wrap as `400 invalid_request` with a
   confusing message (e.g. `unexpected EOF`). There is no `413`.

Note: the issue text also claims the code uses `io.ReadAll` and returns a
`request body exceeds 4194304 bytes` error. Neither is accurate for the current
request path — decoding is already streaming via `json.NewDecoder`, and no
`MaxBytesReader` (the source of that message) is used. The core limitation is real;
these specific details are not.

## Goals

- Admin-configurable JSON body limit, with a separate higher limit for multimodal
  chat endpoints.
- Clear `413 Payload Too Large` on over-limit instead of a silent-truncation `400`.
- Preserve streaming decode (no whole-body `io.ReadAll`) to bound memory under load.
- Keep OpenAI `/v1` contract unchanged for well-formed under-limit requests.

## Non-goals

- Raising `/v1/embeddings` to the multimodal limit (text-only; noted as a follow-up).
- Committing an nginx config to the repo (none exists today). We only document the
  recommended reverse-proxy `client_max_body_size` alignment.

## Design

### 1. Configuration (`backend/internal/server/config.go`)

Add two `int64` fields to `Config`:

```go
MaxJSONRequestBytes       int64 // global default for all JSON endpoints
MaxMultimodalRequestBytes int64 // higher limit for multimodal chat endpoints
```

Read in `ConfigFromEnv`:

| Env var | Default | Notes |
|---|---|---|
| `TOKENHUB_MAX_JSON_REQUEST_BYTES` | `8 << 20` (8 MiB) | global default |
| `TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES` | `32 << 20` (32 MiB) | chat/responses/playground |

New helper `getenvBytes(key string, fallback int64) int64`:

- Accepts a raw integer (`8388608`) or a size suffix, case-insensitive:
  `k`/`kb`/`kib`, `m`/`mb`/`mib`, `g`/`gb`/`gib` (all treated as binary: 1k = 1024).
  This mirrors nginx's `client_max_body_size 32m` spelling.
- On empty, unparseable, or non-positive input → returns `fallback`.
- **Hard ceiling:** a package constant `maxConfigurableRequestBytes = 512 << 20`
  (512 MiB). Any configured value above the ceiling is clamped to the ceiling and a
  warning is logged, guarding against typos (e.g. `32g`) that would risk OOM.

### 2. Decode logic (`backend/internal/server/http.go`)

`decodeJSON` becomes a method on `*Server` (so it can reach `s.config`) and takes the
`http.ResponseWriter` (required by `http.MaxBytesReader`):

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
            return NewHTTPError(413, "payload_too_large",
                fmt.Sprintf("request body exceeds %d bytes", limit))
        }
        return NewHTTPError(400, "invalid_request", err.Error())
    }
    return nil
}
```

`http.MaxBytesReader` streams; peak memory per request is bounded by the decode
target, not the full body, and total bytes read are capped at `limit`.

### 3. Call sites (~37, all identical today)

Every current site is:

```go
if err := decodeJSON(r, &req); err != nil {
    writeError(w, r, NewHTTPError(400, "invalid_request", err.Error()))
    return
}
```

Rewrite mechanically to:

```go
if err := s.decodeJSON(w, r, &req); err != nil {
    writeError(w, r, err)
    return
}
```

`writeError` already renders `*HTTPError` with its own status, so 413 vs 400 flows
through unchanged.

**Multimodal endpoints** use the higher limit explicitly:

- `handleChatCompletions` (`/v1/chat/completions`)
- `handleResponses` (`/v1/responses`)
- `handleAdminPlaygroundChat` (admin console playground)

```go
if err := s.decodeJSONLimit(w, r, &req, s.config.MaxMultimodalRequestBytes); err != nil {
    writeError(w, r, err)
    return
}
```

`/v1/embeddings` keeps the default; flagged as a follow-up if batch text sizes
prove insufficient.

### 4. Tests

- `config_test.go`: table test for `getenvBytes` — raw bytes, each suffix form,
  empty/garbage/zero/negative → fallback, and above-ceiling → clamped.
- Server test (`httptest`): drive `decodeJSONLimit` (or a real endpoint) asserting
  (a) valid under-limit body decodes, (b) over-limit body → `413 payload_too_large`,
  (c) malformed JSON → `400 invalid_request`.

### 5. Docs / env sync

Add both vars, with defaults and a one-line memory/DoS note, to:

- `deploy/.env.example`
- `start.sh` (env passthrough block)
- `docs/deployment.md`, `docs/zh-CN/deployment.md`, `docs/ja/deployment.md` —
  including guidance to set the reverse-proxy `client_max_body_size` at least as
  large as `TOKENHUB_MAX_MULTIMODAL_REQUEST_BYTES`.

## Risks

- **Memory under load:** higher limits raise worst-case buffered bytes per in-flight
  request. Mitigated by streaming decode, the 512 MiB hard ceiling, and conservative
  defaults (8/32 MiB).
- **Signature churn:** 37 call sites change. All are byte-identical, so risk is low;
  `go build` + `go vet` catch any miss.
```
