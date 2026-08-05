package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsAConfig(t *testing.T) {
	path := write(t, `
defaults:
  source: my-source
  digested: my-digested
  hits: 25
  workers: 4
  verbose: true
embed:
  base_url: http://elsewhere:1234
  model: some-model
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Defaults.Source != "my-source" || cfg.Defaults.Digested != "my-digested" {
		t.Errorf("defaults = %+v", cfg.Defaults)
	}
	if cfg.Defaults.Hits != 25 || cfg.Defaults.Workers != 4 || !cfg.Defaults.Verbose {
		t.Errorf("defaults = %+v", cfg.Defaults)
	}
	if cfg.Embed.BaseURL != "http://elsewhere:1234" || cfg.Embed.Model != "some-model" {
		t.Errorf("embed = %+v", cfg.Embed)
	}
	if !cfg.Loaded() {
		t.Error("Loaded() is false for a config that was read")
	}
}

func TestLoadEmptyFileIsValid(t *testing.T) {
	cfg, err := Load(write(t, ""))
	if err != nil {
		t.Fatalf("an empty config should be valid, if pointless: %v", err)
	}
	if !cfg.Loaded() {
		t.Error("an empty config that was found should still count as loaded")
	}
}

// TestLoadRejectsUnknownFields is the KnownFields decision: a typo that silently
// does nothing is worse than a startup failure.
func TestLoadRejectsUnknownFields(t *testing.T) {
	if _, err := Load(write(t, "defaults:\n  hitz: 10\n")); err == nil {
		t.Error("a misspelled field was accepted")
	}
}

func TestLoadExplicitPathMustExist(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("being pointed at a config that is not there should fail rather than be ignored")
	}
}

func TestLoadNoConfigIsNotAnError(t *testing.T) {
	// No explicit path, and nothing beside the test binary.
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("an absent config should not be an error: %v", err)
	}
	if cfg.Loaded() {
		t.Log("a coroner.yaml exists beside the test binary; skipping the zero-value check")
		return
	}
	if cfg.Defaults.Hits != 0 {
		t.Errorf("an absent config produced non-zero defaults: %+v", cfg.Defaults)
	}
}

func TestWriteStarterRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)

	if err := WriteStarter(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteStarter(path); err == nil {
		t.Error("initconfig overwrote an existing config")
	}
}

// TestStarterConfigLoads is worth having on its own: the starter is almost
// entirely comments, and a config that cannot be parsed would only be discovered
// by whoever ran initconfig first.
func TestStarterConfigLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := WriteStarter(path); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("the generated starter config does not parse: %v", err)
	}
	if cfg.Defaults.Source != "source" || cfg.Defaults.Digested != "digested" {
		t.Errorf("the starter config's uncommented defaults changed: %+v", cfg.Defaults)
	}
}
