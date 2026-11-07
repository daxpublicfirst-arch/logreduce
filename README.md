# logreduce

Reduce a build, test or lint log to a small, structured answer: what
failed, where, and the first lines of context. Better than grepping
the log.

Harness-independent: no product names, no model calls, no network.
It reads only the path named by `--log` and writes only under `--out`.

## Example

```sh
logreduce reduce --kind vitest --log test.log --out ./out --json
```

```json
{"ok":true,"tool":"logreduce","version":"0.1.0","kind":"vitest",
 "summary":{"total":42,"passed":40,"failed":2,"errors":0,"skipped":0},
 "findings":[
   {"file":"src/a.test.ts","line":12,"severity":"error",
    "message":"expected 1 to be 2","test":"adds",
    "context":["...","..."]}
 ],
 "unparsed":0,
 "full_log":"/abs/out/full.log",
 "truncated":false,
 "duration_ms":12}
```

`--out` receives `full.log` (a copy of the input log, whole) and
`findings.json` (the same JSON printed to stdout).

## Commands

```text
logreduce reduce --kind <kind> --log <path> --out <dir> [--json] [--request <path>]
logreduce self-test [--json]
logreduce doctor [--json]
logreduce version [--json]
```

`--kind` is one of: `trx`, `vitest`, `playwright`, `tsc`, `eslint`,
`pytest`, `go-test`, `generic`.

Every command supports `--json` and writes one JSON object to stdout
(`reduce` always writes JSON; `version` prints `logreduce 0.1.0` in
plain form, JSON with `--json`).

Optional adapter envelope: `--request <path>` reads

```json
{"protocol":"arabica-plugin-v1","command":"reduce",
 "args":{"kind":"vitest","log":"/abs/test.log","out":"/abs/out"}}
```

instead of flags (explicit flags override envelope fields).

Exit codes: `0` success, `1` findings present (not an error) or
self-test mismatch, `2` usage or input error, `3` tool failure.

On failure one JSON object is printed to stdout and the exit code is
non-zero:

```json
{"ok":false,"error":{"code":"INPUT_MISSING","blocked":"...","fix":"...","if_that_fails":"..."}}
```

Telemetry: one JSON line to stderr per run:

```json
{"tool":"logreduce","version":"0.1.0","command":"reduce",
 "duration_ms":123,"input_bytes":4567,"output_bytes":890,"ok":true}
```

## Parsing

The runner's own JSON or XML reporter is preferred; text is only
regex-parsed where no JSON reporter exists (`tsc`, `go-test` output
lines, `generic`).

| kind | source |
| --- | --- |
| `trx` | .NET TRX XML. `outcome="Failed"` tests; `testName`, first stack frame with file and line, message. Summary from `ResultSummary/Counters`. |
| `vitest` | Vitest JSON reporter. `testResults[].assertionResults[]` with `status === "failed"`; `title`, `fullName`, `failureMessages`, `location`. |
| `playwright` | Playwright JSON reporter. `suites[].specs[].tests[].results[]` with `status === "failed"`; `location.file`, `location.line`; `error.message`. Suites nest. |
| `tsc` | `file(line,col): error TSxxxx: message`; warnings for `warning TS`. |
| `eslint` | ESLint JSON formatter. Severity 2 is error, 1 is warning. |
| `pytest` | `pytest-json-report` output, or JUnit XML as fallback. Failed tests: `outcome === "failed"`; `nodeid`, `call.longrepr`; file and line from the traceback. |
| `go-test` | `go test -json` lines. `Action === "fail"` with `Test`; `Output` for context; file and line from the first `file.go:line:` in the output. |
| `generic` | Text lines containing `error`, `failed`, `failure`, `exception` or `panic`; keeps `path:line` when present; context capped at 5 lines. |

Findings are sorted by file, line, severity, message. Context is
capped at 10 lines per finding (5 for `generic`). Findings are capped
at 1000; when cut, `truncated` is `true`.

`unparsed` counts failure lines that could not be parsed. They are
never dropped: up to 50 of them are kept as findings with `file`
empty and `line` 0, message set to the raw line.

`summary` for test kinds comes from the runner's counters (or is
derived from the tests seen). For lint-like kinds (`tsc`, `eslint`,
`generic`) there are no test counts, so `total` is the finding count,
`failed` / `errors` are the error-severity finding count, and
`passed` / `skipped` are 0.

Input is limited to 200 MB (`limits.input_mb` in `plugin.toml`). If
a log is larger, only the first 200 MB are processed and `truncated`
is set to `true`.

## Setup and testing

Requires Go 1.23+.

```sh
make build          # build bin/logreduce
make test           # go test ./...
make check          # fmt + vet + test + self-test + doctor
./bin/logreduce self-test   # every fixture in fixtures/ must match
./bin/logreduce doctor      # runtime checks
```

`--self-test` runs every fixture in `fixtures/<kind>/` (one per
kind: `input.*` plus `expected.json`) and exits 0 only when every
expected output matches. Comparison normalises `duration_ms` to 0
and `full_log` to a placeholder on both sides, because absolute
paths differ per machine; `expected.json` is otherwise the exact
stdout JSON.

## Release

```sh
make release
```

Builds static linux-amd64 and linux-arm64 binaries in `dist/`,
plus `logreduce-0.1.0-linux-*.tar.gz` tarballs and `dist/sha256sum.txt`.
Binaries are build artifacts and are not committed to git; CI builds
them on every push to `main` (see below).

The `v0.1.0` tag is created from the `main` head after the release
commit lands, e.g. `git tag v0.1.0 && git push --tags`, and a GitHub
Release is made from the `dist/` artifacts. The tag cannot be created
by the Muse GitHub App (its tool catalogue has no create-tag or
create-release operation).

## CI

The CI workflow (`.github/workflows/ci.yml`, in the repo's
accompanying files) runs `make check` and `make release` on every
push to `main`. Note: the Muse GitHub App cannot write
`.github/workflows/` files (GitHub returns 403 for workflow files
unless the app has the `workflows` permission), so that one file
could not be committed from this session — add it from your own
checkout with `git add .github/workflows/ci.yml && git commit &&
git push`, or via the GitHub web UI.

## Layout

```text
logreduce/
  go.mod
  cmd/logreduce/main.go
  internal/reduce/{reduce,trx,vitest,playwright,tsc,eslint,pytest,gotest,generic}.go
  schema/{request,response,error,telemetry}.json
  fixtures/...
  plugin.toml
  README.md
  LICENSE
  CHANGELOG.md
  Makefile
```

## License

MIT. See `LICENSE`.
