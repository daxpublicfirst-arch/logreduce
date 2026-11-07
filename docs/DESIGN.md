# Design of logreduce

`logreduce` is a small, standalone command-line tool (an Arabica plugin,
v1). It reads one build / test / lint log and reduces it to a short,
structured JSON report: which checks failed, in which file, at which
line, with a few lines of context. It never calls a model, never opens
a socket, and never writes outside the directory given with `--out`.

## Commands

| Command | What it does |
|---|---|
| `reduce` | The real work. Reads `--log`, writes `<out>/findings.json` and `<out>/full.log`, prints the same JSON on stdout. Exit 1 when findings exist, 0 when the log is clean, 2 for input/usage errors, 3 for tool failures (including timeout). |
| `self-test` | Runs every fixture in `fixtures/<kind>/` through the binary and compares the output with `fixtures/<kind>/expected.json`. Exit 0 only if all match. |
| `doctor` | Checks the runtime: temp dir writable, static binary, no network needed, fixtures present. Exit 0 when healthy. |
| `version` | Prints `logreduce 0.1.0` (or JSON with `--json`). |

`--self-test` and `--version` are accepted as global flags with no
subcommand. Every command also accepts a `--request` envelope file
(`{"protocol":"arabica-plugin-v1","command":"reduce","args":{...}}`).
Explicit flags override envelope values.

## Data flow

```
log file (≤ 200 MB)
  → os.ReadFile (whole file; see "Limits" for why)
  → parse(kind, data)          # one parser per kind
  → sort findings (file, line, severity, message)
  → cap findings at 1000, cap context, set truncated
  → build summary
  → write <out>/full.log (copy of the processed input)
  → write <out>/findings.json + print the same JSON on stdout
  → one telemetry JSON line on stderr
```

`Reduce` lives in `internal/reduce` and is pure: it takes a path and
an output directory and returns the response bytes. All CLI concerns
(flags, envelope, exit codes, timeout watchdog, telemetry) live in
`cmd/logreduce`. That split exists so the parsers can be unit-tested
and fuzzed without spawning a process, while the CLI contract is
tested end to end through the real binary (see `docs/TESTING.md`).

## Parsers

One parser per kind, in `internal/reduce/<kind>.go`. Each returns a
`parseResult` with findings, a summary, and flags (`truncated`,
`unparsed`). The parsers share helpers: `stripANSI`, `firstLine`,
`truncateMsg`, `contextFromText`, `cleanContext`.

| Kind | Input | How failures are found |
|---|---|---|
| `trx` | .NET TRX XML | `Results/UnitTestResult` with `outcome` Failed/Error. File:line is parsed out of the stack-trace text (`in <file>:line N`). Summary from `ResultSummary/Counters` (skipped = `notExecuted` — TRX has no `skipped` counter). |
| `vitest` | Vitest JSON report | `testResults[].assertionResults[]` with `status == "failed"`. Real reports often have **no `location`** field, so file:line falls back to the first stack frame in `failureMessages[0]` (see `docs/REAL_WORLD.md`). |
| `playwright` | Playwright JSON report | One finding per **test** with `status == "unexpected"`, using its last failed result. File:line comes from `error.location`, `errors[0].location`, `errorLocation`, or the spec's `file`/`line`. There is no `results[].location` in the real format. A `flaky` test passed on its last retry, so it is not a finding. Retries do not multiply findings. |
| `tsc` | `tsc --noEmit` text | Lines `file(line,col): error TSxxxx:` (and the loose `file:line:col:` form). The next lines until the next diagnostic become context. Lines that look like errors but do not parse become raw findings (capped at 50) and count as `unparsed`. |
| `eslint` | ESLint `--format json` | `messages[]` with `severity` 2 = error, 1 = warning. |
| `pytest` | pytest-json-report JSON, or JUnit XML | Failed/error tests. `longrepr` is read from `call`, then `setup`, then `teardown` (a setup failure has no `call.longrepr`). `longrepr` may be a string, a list, or an object with `reprcrash`. File:line comes from the traceback; the test's `lineno` (0-based definition line) is the fallback. |
| `go-test` | `go test -json` | `Action == "output"` events are accumulated per `(Package, Test)`; a `fail` event carries no output of its own. File:line is taken from the accumulated output text (`file_test.go:14:`). |
| `generic` | Any text log | Lines containing a whole failure word (`error`, `failed`, `failure`, `exception`, `panic`, case-insensitive, word-bounded — "terror" does not match). File:line is scanned from `path:line` tokens; a timestamp like `12:34` is not a file:line. Context is the line plus the next 4 lines. |

### Why hand-written scanning, not regex, on the hot path

The generic and tsc parsers were originally written with regular
expressions. A 11 MB generic log took 6–9 s (acceptance: under 2 s);
profiling showed ~68% of CPU in Go regexp backtracking. They were
rewritten with hand-written token/word scanning (commit `b9155d2`).
The same 11 MB log now parses in ~0.2 s. Regex is still used where
the input is small and the pattern is simple (stack-trace frames,
`file.py:line`, JUnit text); it is never used on a per-line basis
over a whole large log. See `docs/DECISIONS.md` (ADR-2).

## Response, errors, telemetry

- **Response** (`schema/response.json`): `{ok, tool, version, kind,
  summary, findings, truncated, unparsed, duration_ms}`. `summary`
  is `{total, passed, failed, errors, skipped}`. For tsc/eslint/
  generic, `total` is the finding count, `failed`/`errors` is the
  error-severity count, and `passed`/`skipped` are 0 — those log
  kinds do not report a pass count, so inventing one would be a lie.
- **Errors** (`schema/error.json`): one JSON object on **stdout**,
  `{ok:false,error:{code,blocked,fix,if_that_fails}}`, exit 2 (usage
  / input) or 3 (tool failure). Codes: `INPUT_MISSING`,
  `INPUT_UNREADABLE`, `UNKNOWN_KIND`, `INVALID_INPUT`,
  `INVALID_REQUEST`, `USAGE`, `TOOL_FAILURE`, `TIMEOUT`.
- **Telemetry** (`schema/telemetry.json`): exactly one JSON line on
  **stderr** per run, with `duration_ms`, `input_bytes`,
  `output_bytes` (0 when the run fails before producing output) and
  `ok`. Telemetry never goes to stdout, so stdout stays parseable.

## Limits

- **Input 200 MB** (`plugin.toml` `limits.input_mb`). Larger input
  is not rejected: the first 200 MB are processed, `truncated` is
  true, and `full.log` contains exactly those 200 MB. The limit is
  enforced by the tool itself, not only by the harness.
- **Timeout 60 s** (`plugin.toml` `limits.timeout_s`). `reduce`
  runs under a 60 s watchdog in the CLI layer: on timeout it prints
  a `TIMEOUT` error (exit 3) instead of being killed with no output.
  A 200 MB generic log takes ~25 s wall on a loaded VM (~6 s CPU;
  the rest is I/O), so the margin is real but not huge — see
  `docs/TESTING.md`.
- **Findings 1000**, **context 10 lines** per finding (5 for
  generic), **unparsed raw findings 50**, **message 500 chars**.
  Caps exist so a pathological log cannot produce an unbounded
  output file. Any cap being hit sets `truncated: true`.
- **Determinism.** Two runs on the same input produce identical
  output except `duration_ms`. Findings are sorted; no map iteration
  order, no timestamps, no random IDs leak into the output.

## Files

```
plugin.toml          # plugin contract: name, commands, permissions, limits
schema/              # JSON schemas for request/response/error/telemetry
fixtures/<kind>/     # hand-written log + expected.json per kind (self-test)
fixtures-real/<kind>/# logs from real runners / public samples (unit tests)
cmd/logreduce/       # CLI: flags, envelope, exit codes, watchdog, telemetry
internal/reduce/     # parsers + Reduce + unit/fuzz/benchmark tests
scripts/validate.py  # checks plugin.toml + schemas against the contract
docs/                # this file, DECISIONS.md, TESTING.md, REAL_WORLD.md
```

Binaries are build artifacts (`make release`): static
linux-amd64/arm64, plus tarballs and `sha256sum.txt` in `dist/`.
They are not committed to git. The release build is reproducible —
two `make release` runs from the same source are byte-identical
(see `docs/DECISIONS.md` ADR-11 and `docs/TESTING.md`).
