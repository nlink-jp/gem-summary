# gem-summary

> **Status:** Scaffolding (Phase 2 of the [RFP](docs/en/gem-summary-rfp.md)).
> Core summarisation is not yet wired — the CLI prints a notice on stderr.
> Track Phase 1 / 2 / 3 progress in [CHANGELOG](CHANGELOG.md).

Single-purpose text summarisation CLI for the `nlink-jp` util-series.
Reads a `.md` / `.txt` file (or stdin) and emits a summary on stdout via
a single Vertex AI Gemini call. For inputs that exceed the model's
effective context window, it falls back to chunked + parallel + merge
summarisation.

Lighter-weight alternative to shell-agent-v2's built-in `analyze-text`,
which uses a sliding-window summariser across several LLM calls — overkill
for ordinary summary requests.

## Why gem-summary

- **Cheaper than analyze-text** for short / medium documents — one LLM
  call versus several.
- **Sufficient for the common case** thanks to Gemini's ~1M-token
  context window.
- **Chunking only when forced** — large inputs (> ~400k tokens) trigger
  a parallel chunk-merge fallback so the tool never silently refuses an
  input.
- **Pipe-friendly** — stdin / stdout / `--json` make it composable with
  the rest of util-series (`gem-transcribe` → `gem-summary`, etc.).

## Installation

```sh
# From source
git clone https://github.com/nlink-jp/gem-summary
cd gem-summary
make build      # → dist/gem-summary
```

Once published, prebuilt binaries for Linux / macOS / Windows are
available from the GitHub Releases page.

## Configuration

`gem-summary` reads `~/.config/gem-summary/config.toml`. Copy
[`config.example.toml`](config.example.toml) and edit, or override any
field via the env vars in the table below.

```toml
[gcp]
project  = "your-project-id"
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

### Environment variable overrides

Precedence: `GEMSUMMARY_*` env > `GOOGLE_CLOUD_*` env > config file > built-in defaults.

| Variable                       | Overrides                  |
|--------------------------------|----------------------------|
| `GEMSUMMARY_PROJECT`           | `[gcp].project`            |
| `GEMSUMMARY_LOCATION`          | `[gcp].location`           |
| `GEMSUMMARY_MODEL`             | `[model].name`             |
| `GEMSUMMARY_DEFAULT_STYLE`     | `[summary].default_style`  |
| `GEMSUMMARY_CHUNK_THRESHOLD`   | `[summary].chunk_threshold`|
| `GEMSUMMARY_CHUNK_SIZE`        | `[summary].chunk_size`     |
| `GEMSUMMARY_CHUNK_OVERLAP`     | `[summary].chunk_overlap`  |
| `GEMSUMMARY_CHUNK_PARALLELISM` | `[summary].chunk_parallelism`|
| `GEMSUMMARY_REQUEST_TIMEOUT`   | `[summary].request_timeout`|

`GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION` are accepted as
final fallbacks so existing GCP-aware shells need no extra plumbing.

### Authentication

gem-summary uses [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials):

```sh
gcloud auth application-default login
```

Requires the **Vertex AI User** role (`roles/aiplatform.user`) on the
target project, and the Vertex AI API enabled.

## Usage

```sh
gem-summary path/to/notes.md            # default style from config
gem-summary --style short notes.md      # 1-3 sentence summary
cat notes.md | gem-summary --style long # detailed multi-paragraph
gem-summary --json notes.md             # structured output
```

### Flags

| Flag                  | Purpose                                              |
|-----------------------|------------------------------------------------------|
| `--style`             | `short` / `medium` / `long` (default: from config)   |
| `--lang`              | Output language (e.g. `ja`, `en`). Default: auto.    |
| `--model`             | Override model name.                                 |
| `--max-input-tokens`  | Hard cap on input size.                              |
| `--chunk-size`        | Tokens per chunk when chunking is triggered.         |
| `--json`              | Structured JSON output.                              |
| `--quiet` / `-q`      | Suppress stderr progress.                            |
| `--config` / `-c`     | Override config file path.                           |
| `--version`           | Print version and exit.                              |

## Integration with shell-agent-v2

Drop the `summary.sh` wrapper into your shell-agent-v2 tools directory
(see `examples/shell_tools/` in the shell-agent-v2 repository). The
script's `@description:` field tells the agent when to prefer
gem-summary over the built-in `analyze-text` — built-in prompts in
shell-agent-v2 stay untouched, which keeps the integration loosely
coupled.

## Documentation

- [`docs/en/gem-summary-rfp.md`](docs/en/gem-summary-rfp.md) — design RFP
- [`docs/ja/gem-summary-rfp.ja.md`](docs/ja/gem-summary-rfp.ja.md) — same, Japanese (primary)
- [`AGENTS.md`](AGENTS.md) — contributor onboarding (build / test / structure)
- [`CHANGELOG.md`](CHANGELOG.md) — release notes

## License

MIT — see [LICENSE](LICENSE).
