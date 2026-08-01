package bundle

import (
	"encoding/json"
	"testing"
	"time"
)

func TestValidateBundle(t *testing.T) {
	bundle := &CanonicalMigrationBundle{
		SchemaVersion: SchemaVersion,
		Source: Source{
			Adapter:        "litellm",
			AdapterVersion: "1.60.0",
		},
		GeneratedAt: time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
	}

	if err := Validate(bundle); err != nil {
		t.Fatalf("validate bundle: %v", err)
	}
}

func TestValidateJSONRejectsMissingFields(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"schema_version": SchemaVersion,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := ValidateJSON(payload); err == nil {
		t.Fatal("expected schema validation to fail")
	}
}

func TestValidateJSONRejectsUnknownFields(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"schema_version": SchemaVersion,
		"source": map[string]any{
			"adapter":         "litellm",
			"adapter_version": "1.60.0",
		},
		"generated_at": "2026-07-23T10:00:00Z",
		"unexpected":   true,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := ValidateJSON(payload); err == nil {
		t.Fatal("expected unknown field validation to fail")
	}
}

func TestValidateJSONRejectsInvalidSecretRef(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"schema_version": SchemaVersion,
		"source": map[string]any{
			"adapter":         "litellm",
			"adapter_version": "1.60.0",
		},
		"generated_at": "2026-07-23T10:00:00Z",
		"providers": []map[string]any{{
			"external_ref": map[string]any{"system": "litellm", "id": "provider/openai"},
			"spec":         map[string]any{"name": "OpenAI", "type": "openai_compatible"},
			"api_key_secret": map[string]any{
				"wrong": "OPENAI_API_KEY",
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := ValidateJSON(payload); err == nil {
		t.Fatal("expected invalid secret ref validation to fail")
	}
}

func TestValidateJSONRejectsProviderSpecUnknownFields(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"schema_version": SchemaVersion,
		"source": map[string]any{
			"adapter":         "litellm",
			"adapter_version": "1.60.0",
		},
		"generated_at": "2026-07-23T10:00:00Z",
		"providers": []map[string]any{{
			"external_ref": map[string]any{"system": "litellm", "id": "provider/openai"},
			"spec": map[string]any{
				"name":       "OpenAI",
				"type":       "openai_compatible",
				"status":     "active",
				"unexpected": true,
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := ValidateJSON(payload); err == nil {
		t.Fatal("expected provider spec unknown field validation to fail")
	}
}

func TestValidateJSONRejectsUserMissingRequiredFields(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"schema_version": SchemaVersion,
		"source": map[string]any{
			"adapter":         "litellm",
			"adapter_version": "1.60.0",
		},
		"generated_at": "2026-07-23T10:00:00Z",
		"users": []map[string]any{{
			"external_ref": map[string]any{"system": "litellm", "id": "user/admin"},
			"spec": map[string]any{
				"username": "admin",
				"status":   "active",
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := ValidateJSON(payload); err == nil {
		t.Fatal("expected user required field validation to fail")
	}
}
