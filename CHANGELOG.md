# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-05-18

First release. gem-summary is a single-purpose Vertex AI
Gemini summarisation CLI for the `nlink-jp` util-series.
Reads a text document from FILE (or stdin) and writes a
summary to stdout. Short / medium documents go through a
single-call fast path; inputs that exceed the configured
threshold fall back to chunked + parallel + merge
summarisation.

This release covers Phases 1 and 2 of the
[approved RFP](docs/en/gem-summary-rfp.md). Phase 3
(shell-agent-v2 integration via a wrapping shell-tool) is
tracked separately and lands in a follow-up commit on the
shell-agent-v2 side; gem-summary itself works standalone as
of v0.1.0.

### Added

- **Core CLI** (`gem-summary [FILE]`) backed by cobra. Input
  is FILE / stdin. Flags: `--style short|medium|long`,
  `--lang` (empty = auto-detect from input), `--model`
  (override config), `--max-input-tokens`, `--chunk-size`,
  `--json`, `--quiet`, `--config`, `--version`, `--help`.
- **Single-call path** — fast and cheap path for documents
  that fit within the configured token threshold. One Vertex
  AI Gemini call, full nlk/guard prompt-injection defence
  (input wrapped in a 128-bit-nonce XML envelope; system
  prompt explicitly forbids following instructions found
  inside the envelope).
- **Chunked path** — automatic fallback for over-threshold
  inputs. Natural-break-aware splitter (paragraph > newline >
  sentence terminator priority within ±10% of the target
  boundary) produces chunks; per-chunk Generate calls fan out
  to a fixed parallelism (config `chunk_parallelism`,
  default 3); per-chunk summaries flow into a single merge
  prompt for the final cohesive summary. Bounded recursion
  (2 levels) handles the pathological case where the merged
  summary itself overruns the threshold; beyond that
  surfaces a clean `ErrInputTooLarge` for the CLI.
- **Configuration** — `~/.config/gem-summary/config.toml`
  using the gem-* unified schema (`[gcp]` + `[model]` +
  tool-specific `[summary]`). Env-var override prefix
  `GEMSUMMARY_*` with the standard GCP fallbacks
  (`GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`).
  Precedence: flag > env > config > built-in defaults.
- **Default model**: `gemini-2.5-flash`. Switch to
  `gemini-3.x` once it GAs (see RFP §7).
- **stderr progress** — model + region, input token estimate,
  per-chunk completion (in chunked path), merge phase,
  final aggregate. `--quiet` / `-q` silences stderr without
  affecting stdout.
- **`--json` output** — structured object with `summary`,
  `chunks`, `tokens_in`, `tokens_out`, `duration_seconds`;
  consumed by future shell-agent-v2 integration.
- **Vertex AI retry handling** — nlk/backoff exponential
  backoff (max 5 attempts, 2s→30s) on documented transient
  errors (429 / 5xx / timeouts / connection-resets). Auth
  and bad-request errors fail fast.
- **THOUGHT preamble filter** — defends against the
  Gemini 2.5 Flash leak where the model occasionally emits a
  text Part with `Thought=false` whose body starts with
  "THOUGHT\n" or "思考\n". Structural `Part.Thought` filter
  plus nlk/strip.ThinkTags pass on the joined text.
- **Tests** — 29 unit tests: config (8) covering precedence
  and malformed-env tolerance; summarizer (19) covering
  style/lang prompt routing, guard wrapping, token
  estimation, single-call happy path, chunked path,
  parallel-error propagation, single-chunk fallthrough,
  chunker boundary preferences, merge prompt construction,
  overlap, forward-progress; vertexai (2 with 12 subtests)
  covering the retry classifier and nil-safety.
- **Documentation** — README.md / README.ja.md with
  env-var override table and shell-agent-v2 integration note;
  AGENTS.md contributor onboarding; CLAUDE.md project
  overview; approved RFP at docs/{en,ja}/gem-summary-rfp{,.ja}.md.

### Compatibility

- Apache Default Credentials required: `gcloud auth
  application-default login` + `roles/aiplatform.user` on
  the GCP project.
- macOS / Linux / Windows binaries shipped (5 platforms via
  `make build-all`).

### Known gaps (tracked, not in v0.1.0)

- shell-agent-v2 wrapping shell-tool (`examples/shell_tools/summary.sh`):
  follow-up commit on the shell-agent-v2 side; gem-summary
  itself is fully usable standalone.
- Gemini 3 migration: revisit default model + thought-signature
  handling after Gemini 3 GA (≥2026-10-16); track via the
  org-wide Gemini 3 migration list.
