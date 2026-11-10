package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssistantProviderConfiguration(t *testing.T) {
	t.Setenv("DRIGA_ASSISTANT_PROVIDER", "")
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg, sources, err := LoadWithSources(path)
	if err != nil || cfg.Assistant.Provider != "auto" || sources["assistant.provider"] != "default" {
		t.Fatalf("default provider: %+v sources=%v err=%v", cfg.Assistant, sources, err)
	}
	for _, provider := range []string{"auto", "claude", "codex", "copilot"} {
		t.Run(provider, func(t *testing.T) {
			if err := os.WriteFile(path, []byte("[assistant]\nprovider = '"+provider+"'\n[adapters.claude]\nenabled = false\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, sources, err := LoadWithSources(path)
			if err != nil || cfg.Assistant.Provider != provider || sources["assistant.provider"] != "file" || cfg.Adapters["claude"].Enabled {
				t.Fatalf("file provider: %+v sources=%v err=%v", cfg, sources, err)
			}
			t.Setenv("DRIGA_ASSISTANT_PROVIDER", "copilot")
			cfg, sources, err = LoadWithSources(path)
			if err != nil || cfg.Assistant.Provider != "copilot" || sources["assistant.provider"] != "env" {
				t.Fatalf("environment override: %+v sources=%v err=%v", cfg.Assistant, sources, err)
			}
		})
	}
}

func TestAssistantProviderRejectsUnsupportedValues(t *testing.T) {
	for _, value := range []string{"", "gemini", "Claude", "auto,codex", "private-invalid-value"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DRIGA_ASSISTANT_PROVIDER", "")
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte("[assistant]\nprovider = '"+value+"'\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "assistant.provider") {
				t.Fatalf("invalid file provider accepted: %v", err)
			}
			if value == "" {
				return // Empty environment values retain the configured/default value.
			}
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DRIGA_ASSISTANT_PROVIDER", value)
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "DRIGA_ASSISTANT_PROVIDER") || strings.Contains(err.Error(), "private-invalid-value") {
				t.Fatalf("invalid environment provider accepted or disclosed: %v", err)
			}
		})
	}
}
