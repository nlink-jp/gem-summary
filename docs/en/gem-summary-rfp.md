# RFP: gem-summary

> Generated: 2026-05-18
> Status: Approved (2026-05-18)

## 1. Problem Statement

When a shell-agent-v2 user asks for a simple summary of a `.md`
or `.txt` document, the LLM picks the built-in `analyze-text`
tool. That tool uses a sliding-window summariser that costs
3–5 LLM calls per turn — overkill for documents that comfortably
fit inside Vertex AI Gemini's multi-hundred-thousand-token
context window, where a single LLM call would do.

**Target user**: shell-agent-v2 daily users primarily, plus
pipeline use from sibling tools in chatops-series and
cybersecurity-series that also want lightweight text
summarisation.

**Solution**: a small CLI that summarises text via a single
Vertex AI Gemini call, reading stdin or a file and writing the
summary to stdout. For inputs that exceed the configured
threshold, it falls back to chunk + parallel-summarise + merge.
shell-agent-v2 invokes it through an `examples/shell_tools/`
script.

---

## 2. Functional Specification

### Commands / API Surface

```
gem-summary [flags] [FILE]

If FILE is omitted or "-", reads from stdin.

Flags:
  --style SHORT|MEDIUM|LONG  Output length preset (default: MEDIUM)
  --lang LANG                Force output language (default: auto-detect)
  --model MODEL              Override config model
  --max-input-tokens N       Hard cap on input size (default: from config)
  --chunk-size N             Window size when fallback chunking activates
                             (default: 200000 tokens)
  --json                     Emit structured JSON
  --quiet                    Suppress stderr progress
  --version
  --help
```

### Input / Output

- **Input**: plain text (`.md` / `.txt` / stdin pipe). Binary
  inputs are rejected.
- **Output (default)**: summary text on stdout. stderr carries
  progress lines (chunk count / per-chunk duration / token
  usage); suppressed by `--quiet`.
- **Output (`--json`)**: structured JSON:
  ```json
  {
    "summary": "…",
    "chunks": 3,
    "tokens_in": 12345,
    "tokens_out": 312,
    "duration_seconds": 18.4
  }
  ```

### Configuration

`~/.config/gem-summary/config.toml` (uniform schema across the
gem-* family):

```toml
[gcp]
project  = "your-project-id"
location = "us-central1"

[model]
name = "gemini-2.5-flash"

[summary]
default_style       = "medium"
chunk_threshold     = 400000  # input token > this triggers chunking
chunk_size          = 200000  # tokens per chunk
chunk_overlap       = 2000    # overlap between adjacent chunks
chunk_parallelism   = 3       # fixed parallelism
output_reserve      = 4096
request_timeout     = 180
```

**Section naming is unified**: `[gcp]` (project / location) +
`[model]` (name) + tool-specific `[summary]` — same shape as
gem-search / gem-image / gem-query / gem-rag / gem-transcribe.

**Environment-variable overrides** (same pattern as the gem-*
family):

| Env var | Overrides |
|---------|-----------|
| `GEMSUMMARY_PROJECT` | `[gcp].project` |
| `GEMSUMMARY_LOCATION` | `[gcp].location` |
| `GEMSUMMARY_MODEL` | `[model].name` |
| `GEMSUMMARY_DEFAULT_STYLE` | `[summary].default_style` |
| `GEMSUMMARY_CHUNK_THRESHOLD` | `[summary].chunk_threshold` |
| `GEMSUMMARY_CHUNK_SIZE` | `[summary].chunk_size` |
| `GEMSUMMARY_CHUNK_OVERLAP` | `[summary].chunk_overlap` |
| `GEMSUMMARY_CHUNK_PARALLELISM` | `[summary].chunk_parallelism` |
| `GEMSUMMARY_REQUEST_TIMEOUT` | `[summary].request_timeout` |

GCP standard env vars (`GOOGLE_CLOUD_PROJECT` /
`GOOGLE_CLOUD_LOCATION`) are recognised as a final fallback
(same behaviour as gem-search / gem-image).

Precedence: `GEMSUMMARY_*` > `GOOGLE_CLOUD_*` > config.toml >
built-in defaults.

Implementation: `BurntSushi/toml` + explicit `os.Getenv`
checks, mirroring gem-search `config.go:73-86` (memory: Vertex
AI config.toml unified).

### External Dependencies

- Vertex AI Gemini API (`google.golang.org/genai` SDK)
- Google Cloud Application Default Credentials (`gcloud auth
  application-default login`)
- nlk (Go util) — `guard` (prompt-injection defence), `backoff`
  (Vertex AI retry), `jsonfix` (only when `--json`)

### Chunk-and-merge algorithm (internal fallback)

1. Estimate input tokens (max of char/4 and word count)
2. If ≤ `chunk_threshold` (default 400k): single-call summary
   → return
3. If > `chunk_threshold`: split by `chunk_size` with overlap →
   summarise each chunk in parallel with
   `chunk_parallelism` (fixed 3) → re-summarise the merged
   chunk-summaries → return
4. If the merge result still exceeds the limit, recurse (in
   practice two levels handle 100MB+ inputs)

---

## 3. Design Decisions

### Language / framework

- **Go** (consistent with existing gem-search / gem-image /
  gem-query / gem-transcribe)
- SDK: `google.golang.org/genai` (vertexai/genai was deprecated
  in 2025-06; memory: genai_go_sdk)
- Config: `BurntSushi/toml` + env override (memory: Vertex AI
  config.toml unified)
- Single binary distribution, `go install` compatible

### Relationship to existing nlink-jp tools

| Tool | Scope | Relationship |
|------|-------|--------------|
| `analyze-text` (shell-agent-v2 built-in) | Multi-LLM-call sliding-window analysis — finding extraction, running summary | `gem-summary` is a **lightweight alternative** for cases that don't need deep analysis |
| `gem-rag` (util-series) | Corpus-wide RAG QA | Different role (retrieval vs. summarisation) |
| `gem-search` (util-series) | Agentic web search | Different domain |
| `gem-transcribe` (util-series) | Audio → text | Composes nicely as a pipeline upstream of gem-summary |
| `data-analyzer` (util-series) | Staged summarisation of large JSON / JSONL | Different input shape (structured) — gem-summary is plain text |

### Explicit non-goals

- Translation (compose with another tool if needed)
- Question answering / Q&A (RAG's domain)
- Structured extraction (keywords / NER — future `gem-shot` if
  ever)
- Image / PDF input (plain text only)
- Streaming output (shell-integration simplicity wins)

### shell-agent-v2 integration is fully encapsulated in the shell-tool's `@description:` field

No built-in prompt changes, no shipped System Rules template,
no pre-seeded Global Memory. Priority order:

1. **Primary**: write a careful `@description:` on the shell
   tool. The LLM reads tool descriptions when deciding which
   tool to call; a clear description that contrasts
   `gem-summary` (fast / cheap / short-to-medium docs) against
   `analyze-text` (deep / multi-window / log audits) is enough
   to drive correct selection.
2. **Fallback (if drift surfaces in practice)**: add an
   `examples/system_rules/` template ("prefer summary for
   summarisation tasks") that users opt into.
3. **Per-turn correction**: the user can always type "use the
   summary tool" — always available.

**Do not modify shell-agent-v2's built-in fixed prompts**:
referencing external / optional tools from a built-in prompt
raises coupling and harms maintainability.

### Prompt injection defence

Full defence via nlk/guard (G1). Input text is wrapped in
nonce tags to suppress prompt-injection attacks. Mandatory for
shell-agent-v2 use, where summary input may come from external
documents the agent ingested.

### Parallelism

Fixed at 3 (H1). Vertex AI rate limits (memory: Gemini API rate
limit — heavy serial requests have produced repeated 429s) make
predictable, modest parallelism the right default.

---

## 4. Development Plan

A single **v0.1.0 release** after all phases complete (J3).
Phases are internal milestones, not release boundaries.

### Phase 1: Core

- Repository scaffold (`github.com/nlink-jp/gem-summary`,
  CONVENTIONS.md-compliant)
- Go module init, `google.golang.org/genai` integration
- `~/.config/gem-summary/config.toml` load + env overrides
- Basic CLI: `gem-summary [FILE]` / `--style` / `--lang` /
  `--model` / `--version` / `--help` / `--quiet`
- Single-call Vertex AI summary (no chunking yet; explicit error
  on over-limit input)
- nlk/guard integration
- stderr progress, `--quiet` suppression
- Unit tests: prompt builder, style preset switching, guard
  wrapping, token estimation
- README.md / README.ja.md / CHANGELOG.md / LICENSE (MIT, matching existing
  tools) / AGENTS.md

**Done when**: short documents summarise end-to-end in a single
call.

### Phase 2: Chunking + JSON output

- Chunk-and-merge implementation (fixed parallelism 3, overlap
  for context bridge)
- `--chunk-size` / `--max-input-tokens` flags
- `--json` output (token usage, chunks, duration)
- Vertex AI rate-limit handling for parallel chunks via
  nlk/backoff (exponential retry)
- Merge prompt design + tests
- Large-input stress tests (1MB log samples etc.)

**Done when**: large documents summarise via chunked path.

### Phase 3: shell-agent-v2 integration + release

- Add `examples/shell_tools/summary.sh` to shell-agent-v2.
  The main design effort here is crafting the `@description:`:
  - Mark it as "fast and cheap, short-to-medium docs"
  - Contrast with analyze-text (deep, multi-window)
  - LLM should be able to select correctly from the tool list
    alone
- Update `examples/shell_tools/README.md` table in
  shell-agent-v2
- E2E smoke (drag-drop `.md` → "summarise this" → summary
  shell-tool is selected)
- Add `gem-summary` submodule to the `util-series` umbrella
- check-org.sh green
- Update `nlink-jp/.github/profile/README.md` tool list
  (alphabetical; memory: org_profile_sort)
- 5-platform binary build (Linux x64 / ARM, macOS x64 / ARM,
  Windows x64; I2)
- GitHub Release v0.1.0 + zip upload

**Done when**: shell-agent-v2 users get the lighter path on
ordinary summary requests.

---

## 5. Required API Scopes / Permissions

### Google Cloud

- **API**: enable Vertex AI API (`aiplatform.googleapis.com`)
  in the project
- **IAM Role**: grant `roles/aiplatform.user` (Vertex AI User)
  to the executing principal
- **Authentication**: Application Default Credentials (ADC)
  - Dev: `gcloud auth application-default login`
  - CI / service account: SA key or Workload Identity
  - Same mechanism as gem-search / gem-image / gem-transcribe

### No additional permissions

- Local file read uses OS permissions only
- No storage writes (stderr / stdout / config read only)
- Network access limited to the Vertex AI endpoint

---

## 6. Series Placement

**Series: util-series**

Reasoning:

- Pipe-friendly CLI (stdin → stdout)
- Data transformation (text → summary text)
- Sits naturally alongside the existing `gem-*` family
  (gem-search / gem-image / gem-rag / gem-query / gem-transcribe)
- LLM-driven but not an interactive CLI client → not
  cli-series
- Not security-focused, not experimental

---

## 7. External Platform Constraints

### Vertex AI Gemini API

- **Rate limit** (memory: Gemini API rate limit): heavy serial
  requests have hit 429s. Mitigated by fixed parallelism 3 and
  nlk/backoff exponential retry.
- **Context window**: gemini-2.5-flash supports 1M input
  tokens, but with output reserve + safety margin the effective
  cap is 400k tokens → triggers chunking above that.
- **Output token limit**: gemini-2.5-flash caps output at
  ~65k tokens; SHORT/MEDIUM/LONG presets bound output through
  prompt instructions.
- **Region**: `global` / `us-central1` etc., configurable
  (default `global`).
- **SDK**: `google-genai` Vertex AI Backend mode
- **Gemini 3 migration**: after Gemini 3 GA (≥2026-10-16),
  switch to `gemini-3.x` and verify `ThoughtSignature` handling.
  **gem-summary must be added to the Gemini 3 migration list**
  (memory: Gemini 3 migration tracks 14 affected tools).

### Distribution

- GitHub Releases (same as other nlink-jp tools)
- 5-platform binary zip (Linux x64 / ARM, macOS x64 / ARM,
  Windows x64)

### shell-agent-v2 integration contract

Follow shell-agent-v2's shell-tool header schema (`@tool:` /
`@description:` / `@category:` / `@timeout:` / `@mitl:`):

- `@category: read` (no side effects beyond authentication)
- `@timeout: 120` (chunked input may take 1–2 minutes; matches
  other gem-* tools)
- `@mitl: off` (read tool, no approval needed)
- `@description:` is the load-bearing field — write it
  carefully (see Design Decisions §"shell-agent-v2 integration
  is fully encapsulated in the shell-tool's `@description:`
  field")

---

## Discussion Log

### Q&A trail

1. **Behaviour above context-window limit** (a): chose
   chunk + parallel + merge. Single-call fast path remains the
   default, chunking is the fallback — that's the core
   differentiation from analyze-text.
2. **Tool scope** (b): summary only. A general-purpose 1-shot
   tool (`gem-shot`) is a separate idea for later.
3. **Style presets** (c): SHORT / MEDIUM / LONG.
4. **JSON output** (d): supported (the shell-tool wrap benefits
   from structured output).
5. **Language behaviour during chunking** (e): `--lang` if
   specified pins the output language; otherwise auto-detect
   from input.
6. **Streaming** (f): no streaming, but stderr progress lines
   like gem-search; `--quiet` suppresses.
7. **Default model**: the initial draft suggested
   `gemini-3.1-pro-preview`; changed to `gemini-2.5-flash`
   pending Gemini 3 GA. Note added to the Gemini 3 migration
   tracking.
8. **Prompt-injection defence level** (g): full nlk/guard
   defence.
9. **Chunk parallelism** (h): fixed 3.
10. **Distribution platforms** (i): 5-platform binaries.
11. **Release cadence** (j): one v0.1.0 after all phases.
12. **shell-agent-v2 guidance changes** (k): initial proposal
    was a one-line addition to the analyze-text descriptor.
    Withdrawn after user pushback: built-in fixed prompts
    should not reference external / optional tools (coupling,
    maintainability). Then further refined: even a System
    Rules template is unnecessary if the shell-tool's
    `@description:` is written well — the description is the
    primary mechanism, System Rules / Global Memory / user
    instruction are progressive fallbacks. Phase 3 dropped
    both the analyze-text edit and the System Rules template;
    the design effort focuses on `@description:` crafting
    instead.

### Key design principle

**Decoupled integration via shell-tool description**:
shell-agent-v2 integration of any external tool is fully
encapsulated in the shell tool's `@description:` field;
built-in fixed prompts stay untouched. This applies not just
to gem-summary but to every future shell-tool integration.
