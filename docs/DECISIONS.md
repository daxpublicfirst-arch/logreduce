# Decisions (ADRs)

Each entry records a decision that a future reader (human or agent)
would otherwise have to guess at or re-derive. Status is "accepted"
unless noted. Dates are 2026-11-08 unless stated.

## ADR-1 — Go, stdlib only, no network

**Decision:** the tool is Go 1.23, standard library only, and never
opens a socket.

**Why:** the tool's job is parsing text; nothing in it needs a
dependency. Stdlib-only means `GOPROXY=off` builds work, there is no
supply-chain surface, and a fresh clone builds with nothing but the
Go toolchain. No network means the tool cannot leak the log it reads,
which is the whole point of a local log reducer.

**Alternatives considered:** a Python tool (slower startup, needs a
runtime the harness may not have); a Rust tool (better perf, but the
team's other tools are Go and the harness is Go-shaped). Rejected:
the performance bar (10 MB under 2 s) is reachable in Go once the
regex problem is fixed (ADR-2).

## ADR-2 — Hand-written scanning on the hot path, not regex

**Decision:** the generic and tsc parsers scan text by hand
(word/token scanning); regex is used only on small inputs (single
stack-trace lines, single `file.py:line` strings).

**Why:** the original regex-based parsers took 6–9 s on an 11 MB
generic log (acceptance: under 2 s). `go tool pprof` showed ~68% of
CPU in `regexp.(*Regexp).backtrack`. After the rewrite
(commit `b9155d2`) the same log parses in ~0.2 s (benchmark:
`BenchmarkGeneric11MB` 212 ms/op). Go's `regexp` is a backtracking
engine for some patterns; hand-written scanning is linear and has no
surprise worst case on adversarial input.

**Trade-off:** hand-written scanners are more code and easier to get
subtly wrong (word boundaries, `path:line` vs `12:34`). Mitigated by
targeted unit tests (`TestGenericWordBoundaries`, `TestTSCLooseForm`)
and fuzz tests for every parser (`internal/reduce/fuzz_test.go`).

**Source:** pprof output from the re-test (see `docs/TESTING.md`).

## ADR-3 — Caps: 1000 findings, 10 context lines, 50 unparsed, 500-char messages

**Decision:** hard caps on output size; any cap hit sets
`truncated: true`.

**Why:** a log is untrusted input. A 200 MB log of repeated errors
could otherwise produce a multi-gigabyte findings file. The caps
bound output to roughly 1000 × (500 chars + 10 lines) — small enough
to hand to a model. `truncated` tells the caller the output is
incomplete instead of silently dropping data.

**Why these numbers:** 1000 findings is far more than a human or a
model can usefully act on in one pass; 10 context lines is enough to
show the failing assertion and its surroundings; 5 for generic (its
context is "the line plus the next few", and generic logs are the
noisiest). The numbers are constants in `internal/reduce/reduce.go`
and are covered by tests (`TestReduceFindingsCap`,
`TestReduceContextCap`, `TestReduceUnparsedCap`).

## ADR-4 — Summary for tsc/eslint/generic: no invented pass counts

**Decision:** for kinds that do not report a pass count, `summary`
is `total = finding count`, `failed`/`errors` = error-severity
count, `passed = 0`, `skipped = 0`.

**Why:** tsc prints diagnostics, eslint prints messages, a generic
log is just text — none of them say how many checks passed.
Inventing a pass count (e.g. `total - failed`) would be wrong: a
tsc run with 0 errors and 500 files "passing" is not 500 passed
checks. Honest zeros are better than plausible-looking lies.

## ADR-5 — One Playwright finding per failing test, not per result

**Decision:** a test with `status == "unexpected"` produces exactly
one finding, from its **last** failed result. A `flaky` test (failed
retries, passed on the last retry) produces no finding and counts as
passed in the summary.

**Why:** Playwright retries a failing test, so one failing test can
have 5 failed results. Emitting one finding per result duplicates
the same failure 5 times and inflates `summary.failed` (the real
report `pw-valid.json` has 2 failing tests but 12 failed results).
The last failed result is the final attempt and the most relevant.
A flaky test's final outcome is "passed", so it is not a finding —
but it is still worth knowing about, which is why `stats.flaky`
exists in the format (we count it as passed; there is no flaky
field in our summary schema).

**Source:** Playwright's own types (`playwright@1.51
types/testReporter.d.ts`: `JSONReportTest.status` is
`expected|unexpected|flaky|skipped`; `JSONReportTestResult.status`
is `passed|failed|timedOut|skipped|interrupted`) and the real
reports in `fixtures-real/playwright/`.

## ADR-6 — Vitest file:line falls back to the stack trace

**Decision:** when a Vitest assertion has no `location` field, parse
the first `file:line:col` frame out of `failureMessages[0]`.

**Why:** the official Vitest JSON example shows `location`, but a
real `vitest run --reporter=json` run produces assertion results
with **no `location` field at all** (verified 2026-11-08; see
`docs/REAL_WORLD.md`). The stack trace in `failureMessages[0]`
does contain the test file and line, and its first frame is the
test itself. Without this fallback, every real Vitest finding would
have no file:line — the tool's main output.

**Trade-off:** the first stack frame could in principle be a
helper, not the test. In practice the first frame of a Vitest
assertion failure is the `expect` call site in the test file.
`location`, when present, takes precedence.

## ADR-7 — pytest: read setup/teardown longrepr; lineno is a fallback

**Decision:** for a failed/error pytest test, read `longrepr` from
`call`, then `setup`, then `teardown` (first non-empty wins), and
use the test item's `lineno` (0-based definition line, so +1) only
when the traceback gives no line.

**Why:** a test that fails in a fixture (setup) has no
`call.longrepr` at all — the original parser returned a finding
with no message and no line for exactly those tests (verified on
the pytest-json-report sample: `test_fail_during_setup` and
`test_fail_during_teardown`). The traceback line is preferred
because it is the failing assert line; `lineno` is only the test
definition line, so it is a fallback, not the primary source.

**Source:** pytest-json-report format docs (pypi.org/project/
pytest-json-report) and `fixtures-real/pytest-sample/input.json`.

## ADR-8 — TRX skipped = notExecuted

**Decision:** `summary.skipped` for TRX is `Counters.notExecuted`.

**Why:** the TRX `Counters` element has no `skipped` attribute
(`total, executed, passed, failed, error, timeout, aborted,
inconclusive, passedButRunAborted, notRunnable, notExecuted,
disconnected, warning, completed, inProgress, pending`).
`notExecuted` is the closest meaning of "skipped".

**Source:** Microsoft docs for the TRX / publish-test-results
format; verified against the real sample in
`fixtures-real/trx/sample.trx` (48 tests, 1 failure).

## ADR-9 — 60 s self-timeout watchdog (TIMEOUT, exit 3)

**Decision:** `reduce` runs under a 60 s watchdog in the CLI layer
(`cmd/logreduce/main.go`: `withTimeout`). On timeout it prints a
`TIMEOUT` error JSON (exit 3) and exits; the harness also enforces
`limits.timeout_s = 60` from `plugin.toml`.

**Why:** the plugin declares a 60 s limit to the harness, and the
harness will kill a run that exceeds it — with no output at all.
A self-timeout gives the caller a parseable error instead of a bare
kill, so the harness can report *why* the run failed. The watchdog
is in the CLI layer, not in the parsers, because the parsers are
pure functions and the timeout is a property of the *run*, not of
the parsing.

**Why not a shorter timeout:** 60 s is the declared limit; a 200 MB
log takes ~25 s wall on a loaded VM, so 60 s leaves real margin
without cutting off legitimate large inputs. See
`docs/TESTING.md` for the measurement.

## ADR-10 — 200 MB input: process the first 200 MB, do not reject

**Decision:** input over 200 MB is not an error. The first 200 MB
are processed, `truncated` is true, and `full.log` holds exactly
those 200 MB.

**Why:** rejecting a 201 MB log would make the tool useless on the
largest real logs, which is exactly when a reducer is most needed.
Processing a prefix is the useful behaviour, and `truncated` makes
the incompleteness explicit. `MaxInputBytes` is a variable (not a
constant) so tests can lower it to exercise the path without a
200 MB file.

## ADR-11 — Reproducible release builds

**Decision:** `make release` produces byte-identical artifacts
across runs from the same source.

**How:** `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`,
`-ldflags "-s -w -buildid="`, and deterministic tarballs
(`--sort=name --mtime='@0' --clamp-mtime --owner=0 --group=0
--numeric-owner --pax-option=delete=atime,delete=ctime`,
`gzip -n`). Verified: two consecutive `make release` runs produce
identical `sha256sum.txt` (see `docs/TESTING.md`).

**Why:** a release whose hash changes every build cannot be
verified by anyone else. Reproducibility is what makes
`sha256sum.txt` meaningful.

**Source:** `go.dev/blog/rebuild` ("Perfectly reproducible Go
builds") and reproducible-builds.org docs on archives.

## ADR-12 — Binaries are artifacts, not committed

**Decision:** `dist/` and `bin/` are gitignored; `make release`
output is never committed.

**Why:** binaries are large, platform-specific, and rebuildable
from source. Committing them would bloat the repo and invite
"which binary matches which commit" confusion. The release
artifacts are distributed as tarballs + `sha256sum.txt`.

## ADR-13 — No tag, no Release, no CI file in the repo (yet)

**Decision:** the repo has no git tag, no GitHub Release, and no
`.github/workflows/ci.yml` — deliberately, for now.

**Why:** the Muse GitHub App cannot do these three things: it has
no create-tag / create-release tool, and writing
`.github/workflows/ci.yml` fails with 403 (the app lacks the
`workflows` permission; verified twice, 2026-11-08). Thomas has
said these steps are out of scope unless he explicitly asks, and
that no browser / web-UI step should be used for them. A copy of
the CI workflow is kept with the release artifacts (see the
deliverables folder in the workspace, `ci.yml`) so Thomas can add
it from his own checkout when he wants it. The workflow itself is
valid YAML and runs `make check`, `make race`, `make cover`,
`make validate` and `make release`.

## ADR-14 — Errors on stdout, telemetry on stderr

**Decision:** error JSON goes to **stdout**, telemetry goes to
**stderr**, exactly one telemetry line per run.

**Why:** stdout is the machine-readable channel: the harness reads
one JSON object from stdout whether the run succeeded or failed.
If errors went to stderr, the harness would have to parse two
streams to learn what happened. Telemetry on stderr keeps stdout
parseable while still giving operators a per-run record
(`duration_ms`, `input_bytes`, `output_bytes`, `ok`).

## ADR-15 — self-test normalises duration and full_log

**Decision:** `self-test` sets `duration_ms` to 0 and `full_log`
to `"FULL_LOG"` on both sides before comparing a run with its
`expected.json`.

**Why:** durations vary run to run, and `full_log` is a copy of the
input (large, and identical by construction). Comparing them would
make self-test flaky and its fixtures huge. The normalisation is
documented in the test itself (`normaliseForCompare`).

## ADR-16 — No config file; flags and --request envelope

**Decision:** the tool takes no config file. Behaviour is set by
flags, or by one `--request` JSON envelope
(`arabica-plugin-v1`); explicit flags override envelope values.

**Why:** a plugin that a harness invokes should be stateless and
explicit. A config file would be one more thing to find, version,
and get wrong. The envelope exists because the harness speaks JSON;
the flags exist because a human at a terminal speaks flags.
