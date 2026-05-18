// Package config loads gem-summary configuration from a TOML
// file with environment-variable overrides.
//
// The schema mirrors the other gem-* tools in the util-series:
// [gcp] (project / location) + [model] (name) + a tool-specific
// [summary] section. Precedence:
//
//	GEMSUMMARY_* env  >  GOOGLE_CLOUD_* env  >  config file  >  built-in defaults
//
// The default config path is ~/.config/gem-summary/config.toml.
// Pass an explicit path to Load() to override that, or pass "" to
// use the default.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Built-in defaults applied when neither the config file nor an
// env var supplies a value. Kept as exported constants so tests
// can reference them by name.
const (
	DefaultLocation         = "us-central1"
	DefaultModel            = "gemini-2.5-flash"
	DefaultStyle            = "medium"
	DefaultChunkThreshold   = 400000
	DefaultChunkSize        = 200000
	DefaultChunkOverlap     = 2000
	DefaultChunkParallelism = 3
	DefaultOutputReserve    = 4096
	DefaultRequestTimeout   = 180
)

// Config is the root configuration for gem-summary. It is
// populated by Load() from the on-disk TOML, then merged with
// env-var and CLI-flag overrides by the caller.
type Config struct {
	GCP     GCPConfig     `toml:"gcp"`
	Model   ModelConfig   `toml:"model"`
	Summary SummaryConfig `toml:"summary"`
}

// GCPConfig holds the Google Cloud project / region that the
// Vertex AI client connects to. Project is required at runtime;
// Location falls back to DefaultLocation.
type GCPConfig struct {
	Project  string `toml:"project"`
	Location string `toml:"location"`
}

// ModelConfig holds the Gemini model name. Other model knobs
// (temperature, top-p, etc.) are intentionally absent — they
// belong inside prompt construction at the summariser level.
type ModelConfig struct {
	Name string `toml:"name"`
}

// SummaryConfig holds gem-summary's tool-specific tunables.
// Token-related fields are all expressed in token units (not
// bytes or characters); the summariser does its own
// estimation when classifying inputs against ChunkThreshold.
type SummaryConfig struct {
	DefaultStyle     string `toml:"default_style"`
	ChunkThreshold   int    `toml:"chunk_threshold"`
	ChunkSize        int    `toml:"chunk_size"`
	ChunkOverlap     int    `toml:"chunk_overlap"`
	ChunkParallelism int    `toml:"chunk_parallelism"`
	OutputReserve    int    `toml:"output_reserve"`
	RequestTimeout   int    `toml:"request_timeout"`
}

// Load reads gem-summary's configuration. If path is empty, the
// default path under ~/.config/gem-summary/config.toml is used;
// when no file exists at that path the function silently
// proceeds with built-in defaults so that an unconfigured
// install can still run when GEMSUMMARY_PROJECT (or
// GOOGLE_CLOUD_PROJECT) is exported.
//
// Env-var overrides are applied AFTER the TOML decode so they
// always take precedence. A missing project ID after all
// resolution sources is treated as a hard error — the binary
// cannot reach Vertex AI without one.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".config", "gem-summary", "config.toml")
		}
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, cfg); err != nil {
				return nil, fmt.Errorf("parse config %s: %w", path, err)
			}
		}
	}

	applyEnvOverrides(cfg)

	if cfg.GCP.Project == "" {
		return nil, fmt.Errorf("GCP project is required: set [gcp].project in config or GEMSUMMARY_PROJECT / GOOGLE_CLOUD_PROJECT env var")
	}

	return cfg, nil
}

// defaults returns a Config populated with built-in values.
// Kept as a constructor (not a package-level var) so tests can
// always start from a fresh, mutation-free baseline.
func defaults() *Config {
	return &Config{
		GCP: GCPConfig{
			Location: DefaultLocation,
		},
		Model: ModelConfig{
			Name: DefaultModel,
		},
		Summary: SummaryConfig{
			DefaultStyle:     DefaultStyle,
			ChunkThreshold:   DefaultChunkThreshold,
			ChunkSize:        DefaultChunkSize,
			ChunkOverlap:     DefaultChunkOverlap,
			ChunkParallelism: DefaultChunkParallelism,
			OutputReserve:    DefaultOutputReserve,
			RequestTimeout:   DefaultRequestTimeout,
		},
	}
}

// applyEnvOverrides walks the documented env-var precedence
// table from the RFP. Tool-specific variables (GEMSUMMARY_*)
// always win over the generic GCP fallbacks; numeric inputs
// that fail to parse fall through to whatever was already
// loaded so a malformed env var never silently zero-fills a
// production setting.
func applyEnvOverrides(cfg *Config) {
	// [gcp].project — tool-specific then generic.
	if v := os.Getenv("GEMSUMMARY_PROJECT"); v != "" {
		cfg.GCP.Project = v
	} else if v := os.Getenv("GOOGLE_CLOUD_PROJECT"); v != "" {
		cfg.GCP.Project = v
	}

	// [gcp].location — tool-specific then generic.
	if v := os.Getenv("GEMSUMMARY_LOCATION"); v != "" {
		cfg.GCP.Location = v
	} else if v := os.Getenv("GOOGLE_CLOUD_LOCATION"); v != "" {
		cfg.GCP.Location = v
	}

	// [model].name
	if v := os.Getenv("GEMSUMMARY_MODEL"); v != "" {
		cfg.Model.Name = v
	}

	// [summary] tunables — numeric ones use parseIntEnv so a
	// malformed value is ignored rather than zeroing the field.
	if v := os.Getenv("GEMSUMMARY_DEFAULT_STYLE"); v != "" {
		cfg.Summary.DefaultStyle = v
	}
	parseIntEnv("GEMSUMMARY_CHUNK_THRESHOLD", &cfg.Summary.ChunkThreshold)
	parseIntEnv("GEMSUMMARY_CHUNK_SIZE", &cfg.Summary.ChunkSize)
	parseIntEnv("GEMSUMMARY_CHUNK_OVERLAP", &cfg.Summary.ChunkOverlap)
	parseIntEnv("GEMSUMMARY_CHUNK_PARALLELISM", &cfg.Summary.ChunkParallelism)
	parseIntEnv("GEMSUMMARY_REQUEST_TIMEOUT", &cfg.Summary.RequestTimeout)
}

// parseIntEnv reads name as an integer and writes it into *dst
// when the env var is set AND parses cleanly. A blank env var
// or a non-integer value leaves *dst untouched.
func parseIntEnv(name string, dst *int) {
	v := os.Getenv(name)
	if v == "" {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return
	}
	*dst = n
}
