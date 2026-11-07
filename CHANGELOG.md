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
- `reduce` runs under a 60 s watchdog: on timeout it prints a
  `TIMEOUT` error and exits 3 instead of being killed with no output.
- Reproducible release builds: two `make release` runs from the same
  source are byte-identical (`-trimpath -buildvcs=false
  -ldflags "-s -w -buildid="`, deterministic tarballs).

### Fixed

- Playwright parser: it read the non-existent `results[].location`
  and emitted one finding per failed *result* (retries duplicated
  failures, and `flaky` tests were reported as failures). It now
  reads `error.location` / `errors[0].location` / `errorLocation`
  / spec `file`/`line`, and emits one finding per `unexpected`
  test from its last failed result. Verified against real Playwright
  JSON reports.
- pytest parser: failures in `setup`/`teardown` had no message and
  no line (only `call.longrepr` was read). It now reads `longrepr`
  from `call`, then `setup`, then `teardown`, and falls back to the
  test item's `lineno`. `longrepr` given as an object with
  `reprcrash` is now parsed instead of being dumped as raw JSON.
- Context truncation was not reported: when a parser cut a finding's
  context to the 10-line cap, `truncated` stayed false. Parsers now
  report the cut and `Reduce` sets `truncated: true`.

### Tests and documentation

- Test coverage 82.3% of statements (`internal/reduce` 86.1%,
  `cmd/logreduce` 73.3% in-process; the CLI is also covered end to
  end through the real binary). `go test -race` passes. Fuzz targets
  for all 8 parsers (`make fuzz`): no crashes in ~500k executions.
- CLI conformance tests (`cmd/logreduce/main_test.go`) run the real
  binary like a harness would: envelope in, JSON out, exit codes,
  `findings.json == stdout`, `full.log == input`, telemetry on stderr.
- Real-world fixtures in `fixtures-real/` from real runner runs
  (pytest, go test, tsc, eslint, vitest) and public samples (TRX,
  Playwright, pytest-json-report).
- Design docs in `docs/`: `DESIGN.md` (architecture),
  `DECISIONS.md` (16 ADRs with the why, trade-offs and sources),
  `TESTING.md` (how to verify, measured numbers),
  `REAL_WORLD.md` (real-log verification log).
- 200 MB input verified: 25 s wall, findings capped at 1000,
  `truncated: true`; input over 200 MB is processed up to the first
  200 MB. ARM64 binary executed under `qemu-aarch64-static`:
  `version`, `self-test` 8/8, `doctor`, `reduce` all pass.
