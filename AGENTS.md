# AGENTS.md — gem-summary

Contributor onboarding. For end-user documentation see
[README.md](README.md). For design rationale see the project RFP
under `docs/en/gem-summary-rfp.md`.

## Project summary

Single-purpose text summariser. Wraps Vertex AI Gemini for the
short-to-medium-document case where one LLM call suffices, with an
automatic chunk + parallel + merge fallback when the input is too big
to fit in one window. Lighter-weight alternative to shell-agent-v2's
built-in `analyze-text`.

## Build & test

```sh
make build          # dist/gem-summary
make test           # or: go test ./...
make build-all      # 5-platform cross-compile (Linux x64/ARM, macOS x64/ARM, Windows x64)
make clean
```

Never invoke `go build` directly — `make build` is the only way to
keep `dist/` clean and the version string consistent (auto-derived
from `git describe --tags`).

## Directory layout

```
gem-summary/
├── main.go                 # cobra entry, version injection
├── cmd/root.go             # CLI flags, RunE
├── internal/
│   ├── config/             # TOML + env-var loading
│   ├── vertexai/           # Gemini client wrapper, retries
│   └── summarizer/         # orchestration, prompt, chunker, merge
├── config.example.toml     # copy to ~/.config/gem-summary/config.toml
├── docs/
│   ├── en/ ja/             # design docs, RFP
└── Makefile
```

## Configuration model

Schema mirrors the other gem-* tools (`gem-search` / `gem-image` /
`gem-query` / `gem-rag` / `gem-transcribe`):

```toml
[gcp]
project  = "..."
location = "us-central1"

[model]
name = "gemini-2.5-flash"

[summary]
default_style     = "medium"
chunk_threshold   = 400000
chunk_size        = 200000
chunk_overlap     = 2000
chunk_parallelism = 3
output_reserve    = 4096
request_timeout   = 180
```

Env-var overrides use the `GEMSUMMARY_*` prefix (no underscore between
`GEM` and `SUMMARY`, matching `GEMSEARCH_*`). `GOOGLE_CLOUD_PROJECT` and
`GOOGLE_CLOUD_LOCATION` are recognised as final fallbacks.

## Coding rules

- Go module: `github.com/nlink-jp/gem-summary`
- Tests live alongside the code (`internal/config/config_test.go` etc.)
- Use the `nlk` library for prompt-injection defence (`guard`) and
  Vertex AI retry handling (`backoff`)
- `--json` output paths must use `nlk/jsonfix` for parsing LLM
  responses that might wrap JSON in markdown fences

## Release process

After Phase 3 completes:

1. Update `CHANGELOG.md`
2. Commit `chore: release vX.Y.Z` → tag → push
3. `gh release create` (no assets)
4. `make build-all` → zip each binary + `README.md` → upload
5. Add as submodule under `nlink-jp/util-series`
6. Update `nlink-jp/.github/profile/README.md` (alphabetical)

## Gotchas

- The agent reading this is a maintainer, not the end user. If you
  see ambiguity in flag behaviour or output shape, check `docs/en/gem-summary-rfp.md`
  for canonical intent — that document is the source of truth for scope decisions.
- Vertex AI's 429 rate-limit behaviour under heavy serial requests is
  documented; chunking parallelism is fixed at 3 to stay under the
  comfortable threshold. Do not raise this casually.
- Gemini 2.5 Flash sometimes emits a leading "THOUGHT" preamble even
  with `IncludeThoughts: false`. Filter via `nlk/strip.ThinkTags` on
  the full response before returning to the user (mirrors
  shell-agent-v2's pattern).
- Gemini 3 migration: add gem-summary to the org-wide migration list
  (see the project memory) once Gemini 3 GAs.
