package config

import (
	"os"
	"path/filepath"
	"testing"
)

// envVars lists every env var Load() consults. clearEnv unsets
// each one and re-isolates HOME / XDG_CONFIG_HOME so the
// developer's real config doesn't leak in. Kept as a package-
// level list so the cleanup function and the setup function
// stay synced by construction.
var envVars = []string{
	"GEMSUMMARY_PROJECT",
	"GEMSUMMARY_LOCATION",
	"GEMSUMMARY_MODEL",
	"GEMSUMMARY_DEFAULT_STYLE",
	"GEMSUMMARY_CHUNK_THRESHOLD",
	"GEMSUMMARY_CHUNK_SIZE",
	"GEMSUMMARY_CHUNK_OVERLAP",
	"GEMSUMMARY_CHUNK_PARALLELISM",
	"GEMSUMMARY_REQUEST_TIMEOUT",
	"GOOGLE_CLOUD_PROJECT",
	"GOOGLE_CLOUD_LOCATION",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range envVars {
		os.Unsetenv(k)
	}
	// Isolate from the developer's real config dir. Without
	// this, TestLoadMissingProject would fail on machines that
	// have a populated ~/.config/gem-summary/config.toml.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() {
		for _, k := range envVars {
			os.Unsetenv(k)
		}
	})
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	os.Setenv("GEMSUMMARY_PROJECT", "test-project")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GCP.Project != "test-project" {
		t.Errorf("Project = %q, want %q", cfg.GCP.Project, "test-project")
	}
	if cfg.GCP.Location != DefaultLocation {
		t.Errorf("Location = %q, want %q", cfg.GCP.Location, DefaultLocation)
	}
	if cfg.Model.Name != DefaultModel {
		t.Errorf("Model = %q, want %q", cfg.Model.Name, DefaultModel)
	}
	if cfg.Summary.DefaultStyle != DefaultStyle {
		t.Errorf("DefaultStyle = %q, want %q", cfg.Summary.DefaultStyle, DefaultStyle)
	}
	if cfg.Summary.ChunkThreshold != DefaultChunkThreshold {
		t.Errorf("ChunkThreshold = %d, want %d", cfg.Summary.ChunkThreshold, DefaultChunkThreshold)
	}
	if cfg.Summary.ChunkParallelism != DefaultChunkParallelism {
		t.Errorf("ChunkParallelism = %d, want %d", cfg.Summary.ChunkParallelism, DefaultChunkParallelism)
	}
}

func TestLoadMissingProject(t *testing.T) {
	clearEnv(t)

	if _, err := Load(""); err == nil {
		t.Fatal("expected error for missing project, got nil")
	}
}

func TestLoadEnvOverrides_ToolSpecific(t *testing.T) {
	clearEnv(t)
	os.Setenv("GEMSUMMARY_PROJECT", "tool-project")
	os.Setenv("GEMSUMMARY_LOCATION", "tool-region")
	os.Setenv("GEMSUMMARY_MODEL", "tool-model")
	os.Setenv("GEMSUMMARY_DEFAULT_STYLE", "long")
	os.Setenv("GEMSUMMARY_CHUNK_THRESHOLD", "123456")
	os.Setenv("GEMSUMMARY_CHUNK_SIZE", "65000")
	os.Setenv("GEMSUMMARY_CHUNK_OVERLAP", "999")
	os.Setenv("GEMSUMMARY_CHUNK_PARALLELISM", "5")
	os.Setenv("GEMSUMMARY_REQUEST_TIMEOUT", "60")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"GCP.Project", cfg.GCP.Project, "tool-project"},
		{"GCP.Location", cfg.GCP.Location, "tool-region"},
		{"Model.Name", cfg.Model.Name, "tool-model"},
		{"Summary.DefaultStyle", cfg.Summary.DefaultStyle, "long"},
		{"Summary.ChunkThreshold", cfg.Summary.ChunkThreshold, 123456},
		{"Summary.ChunkSize", cfg.Summary.ChunkSize, 65000},
		{"Summary.ChunkOverlap", cfg.Summary.ChunkOverlap, 999},
		{"Summary.ChunkParallelism", cfg.Summary.ChunkParallelism, 5},
		{"Summary.RequestTimeout", cfg.Summary.RequestTimeout, 60},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}
}

// TestLoadEnvOverrides_GenericGCPFallback pins the documented
// precedence: GEMSUMMARY_* > GOOGLE_CLOUD_* > config > defaults.
// When the tool-specific env is unset, the generic GCP env should
// fill in. When the tool-specific env IS set, the generic one
// must be ignored even if also present.
func TestLoadEnvOverrides_GenericGCPFallback(t *testing.T) {
	clearEnv(t)
	// Only generic is set — should fill in.
	os.Setenv("GOOGLE_CLOUD_PROJECT", "generic-project")
	os.Setenv("GOOGLE_CLOUD_LOCATION", "generic-region")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GCP.Project != "generic-project" {
		t.Errorf("Project = %q, want %q (generic fallback)", cfg.GCP.Project, "generic-project")
	}
	if cfg.GCP.Location != "generic-region" {
		t.Errorf("Location = %q, want %q (generic fallback)", cfg.GCP.Location, "generic-region")
	}

	// Now add tool-specific — must win.
	os.Setenv("GEMSUMMARY_PROJECT", "tool-project")
	os.Setenv("GEMSUMMARY_LOCATION", "tool-region")
	cfg, err = Load("")
	if err != nil {
		t.Fatalf("Load with tool-specific: %v", err)
	}
	if cfg.GCP.Project != "tool-project" {
		t.Errorf("Project = %q, want tool-specific to win", cfg.GCP.Project)
	}
	if cfg.GCP.Location != "tool-region" {
		t.Errorf("Location = %q, want tool-specific to win", cfg.GCP.Location)
	}
}

func TestLoadTOML(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[gcp]
project = "file-project"
location = "file-region"

[model]
name = "file-model"

[summary]
default_style = "short"
chunk_threshold = 99999
chunk_parallelism = 7
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GCP.Project != "file-project" {
		t.Errorf("Project = %q, want file-project", cfg.GCP.Project)
	}
	if cfg.GCP.Location != "file-region" {
		t.Errorf("Location = %q, want file-region", cfg.GCP.Location)
	}
	if cfg.Model.Name != "file-model" {
		t.Errorf("Model = %q, want file-model", cfg.Model.Name)
	}
	if cfg.Summary.DefaultStyle != "short" {
		t.Errorf("DefaultStyle = %q, want short", cfg.Summary.DefaultStyle)
	}
	if cfg.Summary.ChunkThreshold != 99999 {
		t.Errorf("ChunkThreshold = %d, want 99999", cfg.Summary.ChunkThreshold)
	}
	if cfg.Summary.ChunkParallelism != 7 {
		t.Errorf("ChunkParallelism = %d, want 7", cfg.Summary.ChunkParallelism)
	}
	// Fields not in the TOML should keep defaults.
	if cfg.Summary.ChunkSize != DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want default %d", cfg.Summary.ChunkSize, DefaultChunkSize)
	}
}

// TestLoadEnvBeatsTOML pins that env-var overrides apply AFTER
// the TOML decode, so the env always wins over the file even
// when both supply a value for the same field.
func TestLoadEnvBeatsTOML(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[gcp]
project = "file-project"
location = "file-region"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("GEMSUMMARY_LOCATION", "env-region")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.GCP.Project != "file-project" {
		t.Errorf("Project should come from TOML, got %q", cfg.GCP.Project)
	}
	if cfg.GCP.Location != "env-region" {
		t.Errorf("Location should be overridden by env, got %q", cfg.GCP.Location)
	}
}

// TestParseIntEnv_MalformedKeepsExisting guards the
// parseIntEnv contract: a malformed numeric env var must NOT
// zero-fill the destination; the prior value must survive.
func TestParseIntEnv_MalformedKeepsExisting(t *testing.T) {
	clearEnv(t)
	os.Setenv("GEMSUMMARY_PROJECT", "test-project")
	os.Setenv("GEMSUMMARY_CHUNK_PARALLELISM", "not-a-number")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Summary.ChunkParallelism != DefaultChunkParallelism {
		t.Errorf("ChunkParallelism = %d, want default %d (malformed env should be ignored)",
			cfg.Summary.ChunkParallelism, DefaultChunkParallelism)
	}
}
