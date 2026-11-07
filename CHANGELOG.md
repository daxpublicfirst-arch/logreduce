# Changelog

All notable changes to logreduce will be documented in this file.
The format is based on Keep a Changelog, and this project adheres to
Semantic Versioning.

## [0.1.0] - 2026-11-08

### Added

- First release: `reduce`, `self-test`, `doctor`, `version` commands.
- Parsers for `trx`, `vitest`, `playwright`, `tsc`, `eslint`, `pytest`
  (JSON report and JUnit XML), `go-test` (`go test -json`) and `generic`.
  The `generic` and `tsc` parsers use hand-written scanning instead of
  regular expressions, so an 11 MB log finishes in ~0.5 s.
- `plugin.toml` (`arabica-plugin-v1`), JSON schemas in `schema/`,
  fixtures for every kind in `fixtures/`.
- Deterministic, sorted findings; `--request` envelope support;
  telemetry on stderr; exit codes 0 / 1 / 2 / 3.
- Static linux-amd64 and linux-arm64 builds via `make release`.
