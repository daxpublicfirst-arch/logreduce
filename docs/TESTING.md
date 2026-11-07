# Testing logreduce

How to verify the tool, what each check covers, and what was measured
last. Run everything from the repository root with Go 1.23+.

## The standard gate

```
make check     # fmt + vet + validate + go test + self-test + doctor
```

`make check` is the gate: it must be green before any commit. It is
fast (~3 s warm) because it skips the slow checks below.

| Command | What it does | How long (approx) |
|---|---|---|
| `make check` | gofmt, `go vet`, `make validate`, `go test ./...`, `self-test`, `doctor` | ~3 s warm |
| `make validate` | `scripts/validate.py`: `plugin.toml` fields and all `schema/*.json` parse and carry the contract values (incl. the `TIMEOUT` error code) | <1 s |
| `make cover` | `go test -coverprofile` + `go tool cover -func` | ~10 s |
| `make race` | `go test -race ./...` | ~35 s cold |
| `make fuzz` | every parser fuzzed 30 s each (8 targets) | ~4 min |
| `make release` | static amd64/arm64 binaries + deterministic tarballs + `sha256sum.txt` | ~10 s |

## Coverage (measured 2026-11-08)

- `internal/reduce`: **86.1%** of statements.
- `cmd/logreduce`: **73.3%** of statements.
- Whole module: **82.3%**.

The `cmd/` number is in-process coverage only: the CLI end-to-end
tests run the real binary as a subprocess, and `go cover` cannot see
inside a subprocess. The same CLI paths are therefore covered twice —
once end to end (`TestReduceVitestE2E`, `TestReduceErrorCases`,
`TestReduceRequestEnvelope`, …) and once in-process
(`TestCmdReduceInProcess`, `TestRunDispatch`, …) — so the in-process
number understates what is actually tested. The uncovered remainder
is `main()` (untestable by design — it calls `os.Exit`) and a few
error branches.

`MaxInputBytes` is a variable, not a constant, so tests can lower it
to exercise the 200 MB truncation path without a 200 MB file
(`TestReduceInputLimitTruncation`).

## Unit tests

`internal/reduce/*_test.go` covers every parser against the
hand-written fixtures in `fixtures/<kind>/`, plus:

- caps: `TestReduceFindingsCap` (1500 findings → 1000, truncated),
  `TestReduceContextCap`, `TestReduceUnparsedCap`,
  `TestReduceInputLimitTruncation`;
- scanner edge cases: `TestGenericWordBoundaries` ("terror" is not
  an error; `12:34` is not a file:line),
  `TestTSCLooseForm`, `TestStripANSI`, `TestTRXFileLine`,
  `TestFileLineFromText`, `TestPytestLongreprForms`;
- real-world fixtures: `TestRealWorldFixtures` (see
  `docs/REAL_WORLD.md`).

## CLI conformance tests

`cmd/logreduce/main_test.go` runs the real built binary the way an
adapter/harness would: flags or a `--request` envelope in, one JSON
object on stdout, an exit code, files under `--out`, one telemetry
line on stderr. It checks exit codes (0/1/2/3), error codes
(`INPUT_MISSING`, `UNKNOWN_KIND`, `INVALID_INPUT`, `USAGE`),
`findings.json == stdout` (modulo `duration_ms`),
`full.log == input`, and telemetry fields. `TestWithTimeout` checks
the 60 s watchdog helper with a fast function and a deliberately
slow one.

## Fuzzing

`internal/reduce/fuzz_test.go` has one fuzz target per parser
(`FuzzParseGeneric`, `FuzzParseTSC`, `FuzzParseVitest`,
`FuzzParsePlaywright`, `FuzzParseESLint`, `FuzzParseTRX`,
`FuzzParsePytest`, `FuzzParseGoTest`), seeded with the real fixture
input for that kind. The contract is: a parser must never panic on
arbitrary bytes; it may return an error. A failure writes the
crashing input to `internal/reduce/testdata/fuzz/<name>/` for
reproduction.

Last full run (2026-11-08, 10 s per target, ~500k executions
total): all 8 targets PASS, no crashes. Run longer before a release:
`go test -run='^$' -fuzz=FuzzParseGeneric -fuzztime=5m
./internal/reduce/`.

## Performance (measured 2026-11-08)

- 11 MB generic log (70,000× the fixture): **0.80–0.88 s** wall,
  `duration_ms` 691–853, on a loaded VM (load average ~4). Parse
  benchmark: `BenchmarkGeneric11MB` **212 ms/op**,
  `BenchmarkTSC11MB` **90 ms/op**. Acceptance (10 MB under 2 s)
  passes.
- 200 MB generic log (1,320,000× the fixture, 191 MB on disk):
  **25.1 s** wall, `duration_ms` 24,821, findings capped at 1000,
  `truncated: true`. User CPU was only ~6 s; the rest is I/O
  (reading 191 MB + writing the `full.log` copy). Under the 60 s
  timeout, but the margin depends on disk/load — see ADR-9.
- 210 MB input: only the first 200 MB are processed;
  `full.log` is exactly 200 MB; `truncated: true`.

The original regex-based parsers took 6–9 s on the 11 MB log
(~68% of CPU in regexp backtracking — `go tool pprof`). That was a
real bug, fixed in commit `b9155d2` by hand-written scanning
(ADR-2). An early "~1.1 s" claim for the old code was wrong: it was
measured through a pipe that truncated the run.

## Release verification

- `make release` twice from the same source produces
  **byte-identical** files (`diff` on `dist/sha256sum.txt` is empty).
- `sha256sum -c dist/sha256sum.txt` passes 4/4; both binaries are
  static ELF (amd64 + arm64); the amd64 binary passes `self-test`
  8/8 and `doctor`.
- The arm64 binary was **executed** under
  `qemu-aarch64-static` 7.2.0 (prebuilt binary from
  multiarch/qemu-user-static, no root needed): `version`,
  `self-test` 8/8, `doctor`, and a vitest `reduce` (2 findings)
  all pass. Emulation is slower than native, so qemu is a
  correctness check, not a performance claim.

## Clean-clone verification (how to repeat the full check)

```
git clone https://github.com/daxpublicfirst-arch/logreduce.git
cd logreduce
# fresh Go toolchain, empty caches, GOPROXY=off
make check && make race && make cover && make validate
make release && sha256sum -c dist/sha256sum.txt
# large logs, fuzz, qemu as above
```

The last full clean-clone verification ran 2026-11-08 from a fresh
Go 1.23.4 download (sha256 verified against go.dev) with empty
`GOCACHE`/`GOPATH`: gofmt clean, vet pass, 13+ tests pass,
`make check` pass, self-test 8/8, doctor ok, E2E all 8 kinds,
error cases exit 2 with codes, deterministic output, `make release`
pass. (The test count has grown since — see the current
`go test -count=1 -v` output.)
