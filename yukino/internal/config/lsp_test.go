package config

import "testing"

func TestLSPConfigurationValidation(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"name": "  typescript  ", "command": "typescript-language-server", "languages": map[string]any{".ts": "typescript"}}
	}
	server, err := parseLspServerConfig(base())
	if err != nil || server.Name != "typescript" {
		t.Fatal(server, err)
	}
	for _, key := range []string{"args", "env", "initialization_options", "settings", "timeout_ms"} {
		raw := base()
		raw[key] = nil
		if _, err := parseLspServerConfig(raw); err == nil {
			t.Fatalf("explicit null accepted for %s", key)
		}
	}
	for _, timeout := range []any{0, 99, 60001, 100.5, "1000"} {
		raw := base()
		raw["timeout_ms"] = timeout
		if _, err := parseLspServerConfig(raw); err == nil {
			t.Fatalf("invalid timeout accepted: %v", timeout)
		}
	}
	for _, timeout := range []int{100, 60000} {
		raw := base()
		raw["timeout_ms"] = timeout
		if _, err := parseLspServerConfig(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, languages := range []map[string]any{{}, {"ts": "typescript"}, {".ts": ""}} {
		raw := base()
		raw["languages"] = languages
		if _, err := parseLspServerConfig(raw); err == nil {
			t.Fatal("invalid languages accepted", languages)
		}
	}
}
