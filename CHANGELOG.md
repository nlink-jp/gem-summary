# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.3.2] - 2026-11-24

### Changed

- `make vuln` — and so `make package` — scans every platform in `PLATFORMS`,
  with the environment `build-all` builds each with. It scanned the host's
  build alone, so an advisory in a Linux- or Windows-only package could not
  stop a release.

### Security

- **OpenTelemetry is updated from v1.44.0 to v1.47.0** (`go.opentelemetry.io/otel`,
  `otel/trace`, `otel/metric`; they come in through the Vertex AI SDK).
  `govulncheck` found GO-2026-6974 (CVE-2026-128085, published 2026-11-23:
  attribute value length limits could be bypassed with U+FFFD runes) on the SDK's tracing path, which gem-summary reaches whenever it calls
  Gemini.
  The same update fixes GO-2026-6975 (memory allocation amplification in W3C
  baggage parsing in `go.opentelemetry.io/otel`), whose package gem-summary imports
  but whose affected code it does not call.
  Two advisories remain that gem-summary does not call: GO-2026-6278 in
  `github.com/gorilla/websocket` (imported by the SDK; fixed only in an
  untagged commit) and GO-2026-5932 against `x/crypto/openpgp`
  (unmaintained, no fixed release).

## [0.3.1] - 2026-11-14

### Security

- **`golang.org/x/net` is updated from v0.38.0 to v0.62.0.** The connection
  to Vertex AI ran on x/net's own HTTP/2, not the standard library's: the
  SDK's default client configures it that way when a tool does not supply a
  transport of its own. Measured — a request through the same client
  against a local server was handled by `golang.org/x/net/http2`. Three
  advisories apply to an HTTP/2 client there (GO-2026-4918, -6610, -6611);
  each needs the server end of the connection, which is Google's endpoint
  under TLS. With v0.62.0 built by Go 1.27 the same request is handled by
  the standard library, where they are fixed from Go 1.27.2.
- `grpc` moves from v1.66.2 to v1.83.2, `x/text` to v0.43.0 and `x/crypto`
  to v0.58.0, for advisories `govulncheck` traced through code this build
  does not run (the gRPC path is taken only when S2A is switched on).
  `grpc` v1.83.2 requires `cloud.google.com/go/auth` v0.18.2, so that
  moves from v0.9.3 with it. The Vertex AI SDK is unchanged (v1.57.0).
  The new set was checked against a local server, not against the live
  endpoint.

### Changed

- **Building from source needs Go 1.27.4 or later** (`go.mod`; it was
  1.26.2), so that no build uses a standard library with the HTTP/2
  advisories above.
- **`make package` runs the vulnerability check first** (`make vuln`), so a
  release cannot be built past a finding.

## [0.3.0] - 2026-11-04

### Changed

- **An unknown key in `config.toml` is now an error** that names the key,
  where it used to be skipped without a word. A typo such as
  `default_styel` no longer leaves a setting that seems to do nothing.
  Remove or correct the key the message names.
- **The config file is read by nlink-jp/toml** (v0.1.1) instead of the
  third-party `BurntSushi/toml`, so the tool carries no TOML code from
  outside the organization.

### Fixed

- **`make verify-release` now fails closed.** Its last block chained unzip, the
  packaged binary's `--version` and `spctl` with `&&` and ended the whole chain
  in `|| true`, so a zip that did not unpack or a binary that did not run exited
  0 and the upload proceeded. Each step is now judged on its own, the packaged
  binary's `--version` must contain the tag being released, and only the
  informational `spctl` line may be ignored. Matches the org template
  (CONVENTIONS.md §Code Signing → Verifying a release).
- **The Linux archives no longer carry macOS file metadata.** macOS `tar` wrote
  each bundled file's extended attributes (`com.apple.provenance`, and a Dropbox
  attribute where the tree is synced) into the `.tar.gz` twice: as AppleDouble
  `._` members, which GNU tar extracts as stray `._<name>` files beside the real
  ones, and as `LIBARCHIVE.xattr.*` / `SCHILY.xattr.*` pax headers, which it
  reports as unknown keywords. `make package` now archives with
  `COPYFILE_DISABLE=1 tar --no-xattrs`; each setting stops one of the two.
  Archives already published still carry them; the files themselves are
  unaffected.

### Internal

- `make verify-release` also judges each Linux archive: no AppleDouble or other
  macOS metadata members — listed with `--options 'tar:!mac-ext'`, because a
  plain macOS listing folds `._` members away — no extended attributes as pax
  headers, and exactly the canonical binary, `README.md` and `LICENSE`, compared
  in the C locale.
- The Linux-archive check in `make verify-release` reads each archive's pax
  headers with Python's `tarfile` instead of grepping the decompressed stream,
  which also matched file text that names the keywords (a bundled CHANGELOG,
  for one).

## [0.2.0] - 2026-07-12

### Removed

- **darwin/amd64 (Intel) pre-built binary.** macOS releases now ship
  **arm64 only**, per the org-wide policy (darwin is Apple-Silicon only; no
  universal binaries). Intel Mac users can build from source.

### Changed

- **Linux release archives are now `.tar.gz`** (darwin/windows remain `.zip`),
  per `nlink-jp/.github` CONVENTIONS.md §Release Archive Standard.
- **`LICENSE` is now bundled** in every release archive alongside `README.md`.
- **darwin code-signature identifier** is now the canonical `gem-summary`
  (was `gem-summary-darwin-arm64`), set via `codesign -i` so it stays stable
  after the archived binary is renamed to its canonical name.

No change to the binary's behaviour — a packaging / build-config release.

## [0.1.1] - 2026-05-22

### Changed

- **Darwin releases are now Developer ID signed and Apple-notarized.**
  `gem-summary-v0.1.1-darwin-{amd64,arm64}.zip` carry full Apple
  Developer ID Application signatures and notarization tickets from
  Apple. End users on macOS no longer need to bypass Gatekeeper
  with right-click → Open or `xattr -d com.apple.quarantine` on
  first launch; local users who place `gem-summary` under
  Dropbox-synced (or any other FileProvider-managed) paths are no
  longer killed by macOS's ad-hoc + provenance distrust policy.
  Pipeline: `scripts/codesign-darwin.sh` +
  `scripts/notarize-darwin.sh`, driven by `make package`. Adopts
  the org-wide convention in `nlink-jp/.github` CONVENTIONS.md
  §Code Signing.

No behaviour change to the binary itself — feature-wise this is
identical to v0.1.0.

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
