# CLAUDE.md — gem-summary

**Organization rules (mandatory): https://github.com/nlink-jp/.github/blob/main/CONVENTIONS.md**

## Project overview

Single-purpose text-summarisation CLI for the `nlink-jp` util-series.
Reads a `.md` / `.txt` document (or stdin) and emits a summary on stdout
via one Vertex AI Gemini call. Inputs that exceed the model's effective
context window fall back to a chunked + parallel + merge path.

Lighter-weight alternative to shell-agent-v2's built-in `analyze-text`.
Designed both for standalone use and as the engine behind a
shell-agent-v2 shell-tool (`examples/shell_tools/summary.sh`).

## Non-negotiable rules

- **Tests are mandatory** — write them with the implementation
- **Never `go build` directly** — always `make build` (outputs to `dist/`)
- **Docs in sync** — update `README.md` and `README.ja.md` together
- **Small, typed commits** — `feat:`, `fix:`, `test:`, `chore:`, `docs:`, `refactor:`, `security:`
- **Security first** — prompt-injection defence via `nlk/guard`, no secrets in code

## Build & test

```sh
make build          # → dist/gem-summary
make test           # or: go test ./...
make build-all      # cross-compile 5 platforms
```

## Configuration

Settings load order: built-in defaults → TOML file → env vars → CLI flags.

- **Config file**: `~/.config/gem-summary/config.toml` (or `-c` flag)
- **Env vars**: `GEMSUMMARY_*` (tool-specific) > `GOOGLE_CLOUD_*` (generic fallback)

| Variable                       | Required | Default              | Description                          |
|--------------------------------|----------|----------------------|--------------------------------------|
| `GEMSUMMARY_PROJECT`           | Yes      | —                    | GCP project ID                       |
| `GEMSUMMARY_LOCATION`          | No       | `us-central1`        | Vertex AI region                     |
| `GEMSUMMARY_MODEL`             | No       | `gemini-2.5-flash`   | Model name                           |
| `GEMSUMMARY_DEFAULT_STYLE`     | No       | `medium`             | Output length preset                 |
| `GEMSUMMARY_CHUNK_THRESHOLD`   | No       | `400000`             | Triggers chunking above this many tokens |
| `GEMSUMMARY_CHUNK_SIZE`        | No       | `200000`             | Tokens per chunk                     |
| `GEMSUMMARY_CHUNK_OVERLAP`     | No       | `2000`               | Overlap between adjacent chunks      |
| `GEMSUMMARY_CHUNK_PARALLELISM` | No       | `3`                  | Fixed concurrency                    |
| `GEMSUMMARY_REQUEST_TIMEOUT`   | No       | `180`                | Per-call timeout (seconds)           |

## Key dependencies

- `google.golang.org/genai` — Vertex AI Gemini SDK
- `github.com/nlink-jp/nlk` — `guard` (prompt-injection defence) + `backoff` (Vertex AI retry) + `jsonfix`
- `github.com/spf13/cobra` — CLI framework
- `github.com/BurntSushi/toml` — config file parsing

## Architecture (planned)

- `cmd/` — cobra root command, flag parsing
- `internal/config/` — TOML + env-var configuration
- `internal/vertexai/` — Gemini client wrapper (single-call + retries)
- `internal/summarizer/` — orchestration:
  - `summarizer.go` — entry point, picks single-call vs chunked path
  - `prompt.go` — SHORT / MEDIUM / LONG prompt builder
  - `chunker.go` — token estimation + window splitting
  - `merge.go` — combines chunk summaries into a final summary

## Design references

- [`docs/en/gem-summary-rfp.md`](docs/en/gem-summary-rfp.md) / [`docs/ja/gem-summary-rfp.ja.md`](docs/ja/gem-summary-rfp.ja.md)
  — approved design RFP; canonical source for scope decisions
