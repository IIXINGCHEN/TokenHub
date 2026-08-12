package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type codexFingerprintMode string

const (
	codexFingerprintModeOption = "codex_fingerprint_mode"

	codexFingerprintOff     codexFingerprintMode = "off"
	codexFingerprintDevice  codexFingerprintMode = "device"
	codexFingerprintSession codexFingerprintMode = "session"
	codexFingerprintFull    codexFingerprintMode = "full"
)

type codexFingerprintIDs struct {
	mode                codexFingerprintMode
	installationID      string
	sessionID           string
	threadID            string
	turnID              string
	windowID            string
	turnStartedAtUnixMS int64
}

func codexFingerprintModeForProvider(provider Provider) codexFingerprintMode {
	if strings.TrimSpace(provider.Options["resource_id"]) == "" {
		return codexFingerprintOff
	}
	switch mode := codexFingerprintMode(strings.ToLower(strings.TrimSpace(provider.Options[codexFingerprintModeOption]))); mode {
	case codexFingerprintOff, codexFingerprintDevice, codexFingerprintSession, codexFingerprintFull:
		return mode
	default:
		return codexFingerprintSession
	}
}

func deriveCodexFingerprintUUID(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	var result uuid.UUID
	copy(result[:], digest[:16])
	result[6] = (result[6] & 0x0f) | 0x40
	result[8] = (result[8] & 0x3f) | 0x80
	return result.String()
}

func resolveCodexFingerprintIDs(provider Provider, clientSessionID string) *codexFingerprintIDs {
	mode := codexFingerprintModeForProvider(provider)
	if mode == codexFingerprintOff {
		return nil
	}
	resourceID := strings.TrimSpace(provider.Options["resource_id"])
	ids := &codexFingerprintIDs{
		mode:           mode,
		installationID: deriveCodexFingerprintUUID("tokenhub:codex-installation-id:v1:" + resourceID),
	}
	if mode == codexFingerprintDevice {
		return ids
	}
	ids.sessionID = deriveCodexFingerprintUUID("tokenhub:codex-session-id:v1:" + resourceID)
	clientSessionID = strings.TrimSpace(clientSessionID)
	if mode == codexFingerprintSession && clientSessionID != "" {
		ids.threadID = deriveCodexFingerprintUUID(fmt.Sprintf("tokenhub:codex-thread-id:v1:%s:%s", resourceID, clientSessionID))
	} else {
		ids.threadID = ids.sessionID
	}
	turnID, err := uuid.NewV7()
	if err != nil {
		turnID = uuid.New()
	}
	ids.turnID = turnID.String()
	ids.windowID = ids.threadID + ":0"
	ids.turnStartedAtUnixMS = time.Now().UnixMilli()
	return ids
}

func prepareCodexFingerprintRequest(provider Provider, incoming http.Header, request *ResponsesRequest) *codexFingerprintIDs {
	if request == nil {
		return nil
	}
	clientSessionID, _ := codexSessionIdentifier(incoming, *request)
	ids := resolveCodexFingerprintIDs(provider, clientSessionID)
	if ids != nil && request.raw != nil {
		request.raw = cloneRawJSON(request.raw, 1)
	}
	applyCodexFingerprintClientMetadata(request, ids)
	return ids
}

func applyCodexFingerprintHeaders(headers http.Header, ids *codexFingerprintIDs) {
	if headers == nil || ids == nil {
		return
	}
	headers.Set("x-codex-installation-id", ids.installationID)
	if ids.mode == codexFingerprintDevice {
		rewriteCodexTurnMetadataHeader(headers, map[string]any{
			"installation_id": ids.installationID,
		})
		return
	}
	headers.Set("x-codex-window-id", ids.windowID)
	headers.Set("x-client-request-id", ids.threadID)
	headers.Set("session-id", ids.sessionID)
	headers.Set("session_id", ids.sessionID)
	headers.Set("thread-id", ids.threadID)
	// The original parent belongs to the client's pre-rewrite thread namespace.
	// Forwarding it would leak that identity and create an invalid mixed lineage.
	headers.Del("x-codex-parent-thread-id")
	rewriteCodexTurnMetadataHeader(headers, codexFingerprintTurnMetadata(ids))
}

func rewriteCodexTurnMetadataHeader(headers http.Header, fields map[string]any) {
	raw := strings.TrimSpace(headers.Get("x-codex-turn-metadata"))
	if raw == "" {
		return
	}
	var metadata map[string]any
	if json.Unmarshal([]byte(raw), &metadata) != nil || metadata == nil {
		return
	}
	mergeCodexFingerprintFields(metadata, fields)
	if rebuilt, err := json.Marshal(metadata); err == nil {
		headers.Set("x-codex-turn-metadata", string(rebuilt))
	}
}

func applyCodexFingerprintClientMetadata(request *ResponsesRequest, ids *codexFingerprintIDs) {
	if request == nil || ids == nil {
		return
	}
	if request.raw == nil {
		request.raw = make(map[string]json.RawMessage)
	}
	metadata := map[string]any{}
	if raw, ok := request.raw["client_metadata"]; ok {
		_ = json.Unmarshal(raw, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["x-codex-installation-id"] = ids.installationID
	if ids.mode == codexFingerprintDevice {
		rewriteEmbeddedCodexTurnMetadata(metadata, map[string]any{
			"installation_id": ids.installationID,
		})
	} else {
		metadata["session_id"] = ids.sessionID
		metadata["thread_id"] = ids.threadID
		metadata["turn_id"] = ids.turnID
		metadata["x-codex-window-id"] = ids.windowID
		rewriteEmbeddedCodexTurnMetadata(metadata, codexFingerprintTurnMetadata(ids))
	}
	if rebuilt, err := json.Marshal(metadata); err == nil {
		request.raw["client_metadata"] = rebuilt
	}
}

func rewriteEmbeddedCodexTurnMetadata(metadata map[string]any, fields map[string]any) {
	raw, ok := metadata["x-codex-turn-metadata"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	var embedded map[string]any
	if json.Unmarshal([]byte(raw), &embedded) != nil || embedded == nil {
		return
	}
	mergeCodexFingerprintFields(embedded, fields)
	if rebuilt, err := json.Marshal(embedded); err == nil {
		metadata["x-codex-turn-metadata"] = string(rebuilt)
	}
}

func codexFingerprintTurnMetadata(ids *codexFingerprintIDs) map[string]any {
	return map[string]any{
		"installation_id":         ids.installationID,
		"session_id":              ids.sessionID,
		"thread_id":               ids.threadID,
		"turn_id":                 ids.turnID,
		"window_id":               ids.windowID,
		"turn_started_at_unix_ms": ids.turnStartedAtUnixMS,
	}
}

func mergeCodexFingerprintFields(target map[string]any, fields map[string]any) {
	for key, value := range fields {
		target[key] = value
	}
}
