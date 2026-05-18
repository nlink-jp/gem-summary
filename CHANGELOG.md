# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Scaffolding (Phase 2 of the RFP, in progress)

- Initial project structure under `_wip/gem-summary/`
- Go module, MIT LICENSE, .gitignore
- Cobra-based CLI entry (`gem-summary --help` / `--version` work)
- Makefile with 5-platform `build-all`
- `config.example.toml` (gem-* unified schema: `[gcp]` + `[model]` + `[summary]`)
- Approved RFP committed under `docs/{en,ja}/gem-summary-rfp{,.ja}.md`
- README / README.ja, AGENTS, CONTRIBUTING, CLAUDE skeletons

Phase 1 (core 1-call summarisation), Phase 2 (chunk + merge),
and Phase 3 (shell-agent-v2 integration + release) follow.
v0.1.0 will be cut after Phase 3, per [RFP §4](docs/en/gem-summary-rfp.md).
