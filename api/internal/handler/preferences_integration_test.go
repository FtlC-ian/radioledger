package handler_test

import (
	"context"
	"net/http"
	"testing"
)

func TestIntegration_PreferencesCRUD(t *testing.T) {
	pool, h := setupIntegration(t)

	_, err := pool.Exec(context.Background(), `
		ALTER TABLE users
			ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}'::jsonb
	`)
	if err != nil {
		t.Fatalf("ensure preferences column: %v", err)
	}

	user := createTestUser(t, pool, "prefs")

	status, env := doJSON(t, h, http.MethodGet, "/v1/preferences", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("get preferences failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var initial map[string]any
	decodeData(t, env.Data, &initial)
	if initial["timezone"] != "UTC" {
		t.Fatalf("expected default timezone UTC, got %#v", initial["timezone"])
	}

	status, env = doJSON(t, h, http.MethodPut, "/v1/preferences", user.ID, map[string]any{
		"display_name":     "Dex Tester",
		"timezone":         "America/Chicago",
		"default_grid":     "EM12",
		"default_band":     "20m",
		"default_mode":     "FT8",
		"default_power":    50,
		"ui_theme":         "light",
		"dedup_window":     45,
		"sync_enabled":     true,
		"desktop_udp_port": 2237,
		"desktop_rig_port": 4532,
	})
	if status != http.StatusOK || !env.Success {
		t.Fatalf("update preferences failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	status, env = doJSON(t, h, http.MethodGet, "/v1/preferences", user.ID, nil)
	if status != http.StatusOK || !env.Success {
		t.Fatalf("get preferences after update failed: status=%d success=%v error=%q", status, env.Success, env.Error)
	}

	var updated map[string]any
	decodeData(t, env.Data, &updated)

	assertPreference(t, updated, "display_name", "Dex Tester")
	assertPreference(t, updated, "timezone", "America/Chicago")
	assertPreference(t, updated, "default_grid", "EM12")
	assertPreference(t, updated, "default_band", "20M")
	assertPreference(t, updated, "default_mode", "FT8")
	assertPreference(t, updated, "ui_theme", "light")
	assertPreferenceNumber(t, updated, "dedup_window", 45)
	assertPreferenceBool(t, updated, "sync_enabled", true)
}

func assertPreference(t *testing.T, payload map[string]any, key, want string) {
	t.Helper()
	if got, ok := payload[key].(string); !ok || got != want {
		t.Fatalf("%s mismatch: got=%#v want=%q", key, payload[key], want)
	}
}

func assertPreferenceNumber(t *testing.T, payload map[string]any, key string, want float64) {
	t.Helper()
	got, ok := payload[key].(float64)
	if !ok || got != want {
		t.Fatalf("%s mismatch: got=%#v want=%v", key, payload[key], want)
	}
}

func assertPreferenceBool(t *testing.T, payload map[string]any, key string, want bool) {
	t.Helper()
	got, ok := payload[key].(bool)
	if !ok || got != want {
		t.Fatalf("%s mismatch: got=%#v want=%v", key, payload[key], want)
	}
}
