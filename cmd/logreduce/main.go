// Command logreduce: reduce a build, test or lint log to findings.
//
// Usage:
//
//	logreduce reduce --kind <kind> --log <path> --out <dir> [--json] [--request <path>]
//	logreduce self-test [--json]
//	logreduce doctor [--json]
//	logreduce version [--json]
//	logreduce --version | --self-test | --help
//
// Exit codes: 0 success, 1 findings present (reduce) / self-test mismatch,
// 2 usage or input error, 3 tool failure. On failure one JSON object is
// printed to stdout. Telemetry (one JSON line) goes to stderr per run.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daxpublicfirst-arch/logreduce/internal/reduce"
)

const usageText = `logreduce 0.1.0 — reduce a build, test or lint log to findings

Usage:
  logreduce reduce --kind <kind> --log <path> --out <dir> [--json] [--request <path>]
  logreduce self-test [--json]
  logreduce doctor [--json]
  logreduce version [--json]

Kinds: trx, vitest, playwright, tsc, eslint, pytest, go-test, generic

Exit codes: 0 success, 1 findings present, 2 usage or input error, 3 tool failure.
`

// reduceTimeout is the plugin limit (plugin.toml limits.timeout_s).
// The harness also enforces it; the tool enforces it itself so a
// timeout produces a parseable error JSON (code TIMEOUT, exit 3)
// instead of the process being killed with no output at all.
const reduceTimeout = 60 * time.Second

var errTimeout = errors.New("reduce timed out")

// withTimeout runs fn and returns errTimeout if it does not finish
// within timeout. fn may keep running in its goroutine after a
// timeout; the caller exits the process right after, which stops it.
func withTimeout(timeout time.Duration, fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return errTimeout
	}
}

type telemetry struct {
	Tool        string `json:"tool"`
	Version     string `json:"version"`
	Command     string `json:"command"`
	DurationMs  int64  `json:"duration_ms"`
	InputBytes  int64  `json:"input_bytes"`
	OutputBytes int64  `json:"output_bytes"`
	OK          bool   `json:"ok"`
}

func emitTelemetry(cmd string, start time.Time, inputBytes, outputBytes int64, ok bool) {
	t := telemetry{Tool: reduce.ToolName, Version: reduce.Version, Command: cmd, DurationMs: time.Since(start).Milliseconds(), InputBytes: inputBytes, OutputBytes: outputBytes, OK: ok}
	b, _ := json.Marshal(t)
	fmt.Fprintln(os.Stderr, string(b))
}

func printError(err *reduce.ToolError) int {
	code, _ := printErrorLen(err)
	return code
}

// printErrorLen prints the error JSON and returns its exit code and
// the byte length written, so telemetry can report output_bytes.
func printErrorLen(err *reduce.ToolError) (int, int64) {
	resp := reduce.ErrorResponse{OK: false, Error: reduce.ErrorDetail{Code: err.Code, Blocked: err.Blocked, Fix: err.Fix, IfThatFails: err.IfThatFails}}
	b, _ := json.Marshal(resp)
	n, _ := fmt.Println(string(b))
	return err.Exit, int64(n)
}

func printErrorGeneric(code, blocked, fix, ifThatFails string) int {
	return printError(&reduce.ToolError{Code: code, Blocked: blocked, Fix: fix, IfThatFails: ifThatFails, Exit: 2})
}

func main() {
	start := time.Now()
	code := run(start)
	os.Exit(code)
}

func run(start time.Time) int {
	args := os.Args[1:]
	// Global flags with no subcommand.
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText)
		return printErrorGeneric("USAGE", "No command given.", "Run `logreduce reduce --kind <kind> --log <path> --out <dir>` or `logreduce --help`.", "Run `logreduce --help` for the full usage.")
	}
	// Normalise --flag aliases to subcommands.
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(usageText)
		return 0
	case "--version", "-v", "-version":
		return cmdVersion(args[1:], start)
	case "--self-test":
		return cmdSelfTest(args[1:], start)
	case "version":
		return cmdVersion(args[1:], start)
	case "self-test":
		return cmdSelfTest(args[1:], start)
	case "doctor":
		return cmdDoctor(args[1:], start)
	case "reduce":
		return cmdReduce(args[1:], start)
	default:
		fmt.Fprint(os.Stderr, usageText)
		return printErrorGeneric("USAGE", "Unknown command: "+args[0], "Use one of: reduce, self-test, doctor, version.", "Run `logreduce --help` for the full usage.")
	}
}

// --- flags ---

type flagSet struct {
	vals  map[string]string
	bools map[string]bool
	rest  []string
}

func parseFlags(args []string) (*flagSet, *reduce.ToolError) {
	fs := &flagSet{vals: map[string]string{}, bools: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			if a == "-h" || a == "--help" {
				fs.bools["help"] = true
				continue
			}
			fs.rest = append(fs.rest, a)
			continue
		}
		name := strings.TrimPrefix(a, "--")
		if idx := strings.Index(name, "="); idx >= 0 {
			fs.vals[name[:idx]] = name[idx+1:]
			continue
		}
		switch name {
		case "json", "help":
			fs.bools[name] = true
		case "kind", "log", "out", "request":
			if i+1 >= len(args) {
				return nil, &reduce.ToolError{Code: "USAGE", Blocked: "Flag --" + name + " needs a value.", Fix: "Pass --" + name + " <value>.", IfThatFails: "Run `logreduce --help` for the full usage.", Exit: 2}
			}
			i++
			fs.vals[name] = args[i]
		default:
			return nil, &reduce.ToolError{Code: "USAGE", Blocked: "Unknown flag: --" + name, Fix: "Remove the flag or check its spelling.", IfThatFails: "Run `logreduce --help` for the full usage.", Exit: 2}
		}
	}
	return fs, nil
}

// --- version ---

func cmdVersion(args []string, start time.Time) int {
	fs, err := parseFlags(args)
	if err != nil {
		return printError(err)
	}
	if fs.bools["help"] {
		fmt.Print(usageText)
		return 0
	}
	if len(fs.rest) > 0 {
		return printErrorGeneric("USAGE", "version takes no arguments.", "Run `logreduce version` or `logreduce version --json`.", "Run `logreduce --help` for the full usage.")
	}
	var out []byte
	if fs.bools["json"] {
		b, _ := json.Marshal(map[string]any{"ok": true, "tool": reduce.ToolName, "version": reduce.Version})
		out = append(b, '\n')
	} else {
		out = []byte(reduce.ToolName + " " + reduce.Version + "\n")
	}
	fmt.Print(string(out))
	emitTelemetry("version", start, 0, int64(len(out)), true)
	return 0
}

// --- doctor ---

type check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func cmdDoctor(args []string, start time.Time) int {
	fs, err := parseFlags(args)
	if err != nil {
		return printError(err)
	}
	checks := []check{}
	// 1. Can create and remove a temp directory (writable scratch).
	tmp, merr := os.MkdirTemp("", "logreduce-doctor-")
	if merr != nil {
		checks = append(checks, check{"writable-temp", false, merr.Error()})
	} else {
		os.RemoveAll(tmp)
		checks = append(checks, check{"writable-temp", true, "can create and remove a temp directory"})
	}
	// 2. Runtime: Go-built static binary needs no interpreter.
	checks = append(checks, check{"runtime", true, "static binary, no runtime dependency"})
	// 3. No network is required; verify by construction (no net imports).
	checks = append(checks, check{"network", true, "no network required; tool never opens a socket"})
	// 4. Fixtures present when run from a source checkout (optional).
	if _, serr := os.Stat("fixtures"); serr == nil {
		checks = append(checks, check{"fixtures", true, "fixtures/ present in current directory"})
	} else {
		checks = append(checks, check{"fixtures", true, "fixtures/ not in current directory (not required for installed binary)"})
	}
	allOK := true
	for _, c := range checks {
		if !c.OK {
			allOK = false
		}
	}
	var out []byte
	if fs.bools["json"] {
		b, _ := json.Marshal(map[string]any{"ok": allOK, "tool": reduce.ToolName, "version": reduce.Version, "command": "doctor", "checks": checks})
		out = append(b, '\n')
	} else {
		var sb strings.Builder
		for _, c := range checks {
			status := "ok"
			if !c.OK {
				status = "FAIL"
			}
			fmt.Fprintf(&sb, "%s %s: %s\n", status, c.Name, c.Detail)
		}
		if allOK {
			sb.WriteString("doctor: ok\n")
		} else {
			sb.WriteString("doctor: FAILED\n")
		}
		out = []byte(sb.String())
	}
	fmt.Print(string(out))
	code := 0
	if !allOK {
		code = 3
	}
	emitTelemetry("doctor", start, 0, int64(len(out)), allOK)
	return code
}

// --- reduce ---

type requestEnvelope struct {
	Protocol string `json:"protocol"`
	Command  string `json:"command"`
	Args     struct {
		Kind string `json:"kind"`
		Log  string `json:"log"`
		Out  string `json:"out"`
	} `json:"args"`
}

func cmdReduce(args []string, start time.Time) int {
	fs, ferr := parseFlags(args)
	if ferr != nil {
		code, n := printErrorLen(ferr)
		emitTelemetry("reduce", start, 0, n, false)
		return code
	}
	if fs.bools["help"] {
		fmt.Print(usageText)
		return 0
	}
	kind, logPath, outDir := fs.vals["kind"], fs.vals["log"], fs.vals["out"]
	// --request envelope supplies kind/log/out; explicit flags win.
	if reqPath := fs.vals["request"]; reqPath != "" {
		raw, err := os.ReadFile(reqPath)
		if err != nil {
			code, n := printErrorLen(&reduce.ToolError{Code: "INPUT_MISSING", Blocked: "Request file not found: " + reqPath, Fix: "Pass an existing file with --request <path>.", IfThatFails: "Use flags instead: --kind <kind> --log <path> --out <dir>.", Exit: 2})
			emitTelemetry("reduce", start, 0, n, false)
			return code
		}
		var env requestEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			code, n := printErrorLen(&reduce.ToolError{Code: "INVALID_REQUEST", Blocked: "Request file is not valid JSON: " + err.Error(), Fix: "Fix the JSON in the --request file.", IfThatFails: "Use flags instead: --kind <kind> --log <path> --out <dir>.", Exit: 2})
			emitTelemetry("reduce", start, 0, n, false)
			return code
		}
		if env.Protocol != "" && env.Protocol != "arabica-plugin-v1" {
			code, n := printErrorLen(&reduce.ToolError{Code: "INVALID_REQUEST", Blocked: "Unsupported protocol in request file: " + env.Protocol, Fix: "Use protocol \"arabica-plugin-v1\".", IfThatFails: "Use flags instead: --kind <kind> --log <path> --out <dir>.", Exit: 2})
			emitTelemetry("reduce", start, 0, n, false)
			return code
		}
		if env.Command != "" && env.Command != "reduce" {
			code, n := printErrorLen(&reduce.ToolError{Code: "INVALID_REQUEST", Blocked: "Unsupported command in request file: " + env.Command, Fix: "Use command \"reduce\" in the --request file.", IfThatFails: "Use flags instead: --kind <kind> --log <path> --out <dir>.", Exit: 2})
			emitTelemetry("reduce", start, 0, n, false)
			return code
		}
		if kind == "" {
			kind = env.Args.Kind
		}
		if logPath == "" {
			logPath = env.Args.Log
		}
		if outDir == "" {
			outDir = env.Args.Out
		}
	}
	if kind == "" || logPath == "" || outDir == "" {
		code, n := printErrorLen(&reduce.ToolError{Code: "USAGE", Blocked: "reduce needs --kind, --log and --out.", Fix: "Run `logreduce reduce --kind <kind> --log <path> --out <dir>`.", IfThatFails: "Run `logreduce --help` for the full usage.", Exit: 2})
		emitTelemetry("reduce", start, 0, n, false)
		return code
	}
	if len(fs.rest) > 0 {
		code, n := printErrorLen(&reduce.ToolError{Code: "USAGE", Blocked: "Unexpected argument: " + fs.rest[0], Fix: "Remove the extra argument.", IfThatFails: "Run `logreduce --help` for the full usage.", Exit: 2})
		emitTelemetry("reduce", start, 0, n, false)
		return code
	}
	// Input size for telemetry (best effort; Reduce re-checks).
	var inputBytes int64
	if info, err := os.Stat(logPath); err == nil {
		inputBytes = info.Size()
	}
	var out []byte
	var err error
	if werr := withTimeout(reduceTimeout, func() error {
		var rerr error
		_, out, rerr = reduce.Reduce(kind, logPath, outDir, start)
		err = rerr
		return rerr
	}); werr != nil {
		if werr == errTimeout {
			code, n := printErrorLen(&reduce.ToolError{Code: "TIMEOUT", Blocked: "reduce timed out after 60s", Fix: "Split the log into smaller parts and run logreduce on each part.", IfThatFails: "Run with --kind generic on the largest part first.", Exit: 3})
			emitTelemetry("reduce", start, inputBytes, n, false)
			return code
		}
		err = werr
	}
	if err != nil {
		if te, ok := err.(*reduce.ToolError); ok {
			code, n := printErrorLen(te)
			emitTelemetry("reduce", start, inputBytes, n, false)
			return code
		}
		code, n := printErrorLen(&reduce.ToolError{Code: "TOOL_FAILURE", Blocked: err.Error(), Fix: "Retry the run.", IfThatFails: "Report this as a bug with the log file attached.", Exit: 3})
		emitTelemetry("reduce", start, inputBytes, n, false)
		return code
	}
	// Reduce always emits JSON on stdout, --json or not.
	fmt.Print(string(out))
	emitTelemetry("reduce", start, inputBytes, int64(len(out)), true)
	// Exit 1 when findings are present (not an error), 0 otherwise.
	// Re-read the count from the response bytes is wasteful; parse the
	// findings count from the written file is also wasteful — instead
	// decode the summary from the response we already have by counting
	// via a light unmarshal of the summary only.
	var probe struct {
		Summary struct {
			Failed int `json:"failed"`
			Errors int `json:"errors"`
		} `json:"summary"`
		Findings []json.RawMessage `json:"findings"`
	}
	_ = json.Unmarshal(out, &probe)
	if len(probe.Findings) > 0 {
		return 1
	}
	return 0
}

// --- self-test ---

func cmdSelfTest(args []string, start time.Time) int {
	fs, ferr := parseFlags(args)
	if ferr != nil {
		return printError(ferr)
	}
	// Find fixtures/ relative to cwd, or next to the binary's source
	// checkout (walk up from cwd).
	fixturesDir := "fixtures"
	if _, err := os.Stat(fixturesDir); err != nil {
		// Try the directory containing the executable's parent.
		exe, _ := os.Executable()
		cand := filepath.Join(filepath.Dir(exe), "..", "fixtures")
		if _, err := os.Stat(cand); err == nil {
			fixturesDir = cand
		}
	}
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		emitTelemetry("self-test", start, 0, 0, false)
		return printError(&reduce.ToolError{Code: "INPUT_MISSING", Blocked: "fixtures/ directory not found: " + fixturesDir, Fix: "Run self-test from the repository root, where fixtures/ lives.", IfThatFails: "Reinstall logreduce from the repository.", Exit: 2})
	}
	type result struct {
		Kind   string `json:"kind"`
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	var results []result
	allOK := true
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		kind := e.Name()
		ok, detail := runFixture(fixturesDir, kind)
		results = append(results, result{Kind: kind, OK: ok, Detail: detail})
		if !ok {
			allOK = false
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Kind < results[j].Kind })
	var out []byte
	if fs.bools["json"] {
		b, _ := json.Marshal(map[string]any{"ok": allOK, "tool": reduce.ToolName, "version": reduce.Version, "command": "self-test", "results": results})
		out = append(b, '\n')
	} else {
		var sb strings.Builder
		for _, r := range results {
			status := "ok"
			if !r.OK {
				status = "FAIL"
			}
			fmt.Fprintf(&sb, "%s %s: %s\n", status, r.Kind, r.Detail)
		}
		if allOK {
			sb.WriteString("self-test: ok\n")
		} else {
			sb.WriteString("self-test: FAILED\n")
		}
		out = []byte(sb.String())
	}
	fmt.Print(string(out))
	code := 0
	if !allOK {
		code = 1
	}
	emitTelemetry("self-test", start, 0, int64(len(out)), allOK)
	return code
}

// runFixture runs one fixture kind: input.* -> reduce into a temp out
// dir, then compares findings.json with expected.json after normalising
// duration_ms and full_log on both sides.
func runFixture(fixturesDir, kind string) (bool, string) {
	dir := filepath.Join(fixturesDir, kind)
	files, err := os.ReadDir(dir)
	if err != nil {
		return false, "cannot read fixture dir: " + err.Error()
	}
	var inputPath, expectedPath string
	for _, f := range files {
		name := f.Name()
		if name == "expected.json" {
			expectedPath = filepath.Join(dir, name)
		} else if strings.HasPrefix(name, "input.") {
			inputPath = filepath.Join(dir, name)
		}
	}
	if inputPath == "" {
		return false, "no input.* file in " + dir
	}
	if expectedPath == "" {
		return false, "no expected.json in " + dir
	}
	tmpOut, err := os.MkdirTemp("", "logreduce-selftest-")
	if err != nil {
		return false, "cannot create temp out dir: " + err.Error()
	}
	defer os.RemoveAll(tmpOut)
	_, outBytes, err := reduce.Reduce(kind, inputPath, tmpOut, time.Now())
	if err != nil {
		return false, "reduce failed: " + err.Error()
	}
	// Read the written findings.json (must equal stdout bytes).
	written, err := os.ReadFile(filepath.Join(tmpOut, "findings.json"))
	if err != nil {
		return false, "cannot read findings.json: " + err.Error()
	}
	if string(written) != string(outBytes) {
		return false, "findings.json differs from stdout"
	}
	if _, err := os.Stat(filepath.Join(tmpOut, "full.log")); err != nil {
		return false, "full.log missing"
	}
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		return false, "cannot read expected.json: " + err.Error()
	}
	actualNorm, err := normaliseForCompare(outBytes)
	if err != nil {
		return false, "cannot normalise actual output: " + err.Error()
	}
	expectedNorm, err := normaliseForCompare(expected)
	if err != nil {
		return false, "cannot normalise expected.json: " + err.Error()
	}
	if actualNorm != expectedNorm {
		return false, "output does not match expected.json\n--- expected ---\n" + expectedNorm + "\n--- actual ---\n" + actualNorm
	}
	return true, "matches expected.json"
}

// normaliseForCompare canonicalises JSON with duration_ms=0 and
// full_log="FULL_LOG" on both sides.
func normaliseForCompare(b []byte) (string, error) {
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	v["duration_ms"] = float64(0)
	v["full_log"] = "FULL_LOG"
	canon, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	// Re-marshal with sorted keys is guaranteed by encoding/json for maps.
	return string(canon), nil
}
