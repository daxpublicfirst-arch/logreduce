package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CLI end-to-end tests: they run the real built binary the way an
// adapter/harness would — flags or a --request envelope in, one JSON
// object on stdout, an exit code, files under --out, telemetry on
// stderr. These are the conformance tests for the plugin contract in
// plugin.toml and schema/.

var testBinary string
var repoRoot string

func TestMain(m *testing.M) {
	var err error
	repoRoot, err = filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	dir, err := os.MkdirTemp("", "logreduce-e2e-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	testBinary = filepath.Join(dir, "logreduce")
	cmd := exec.Command("go", "build", "-o", testBinary, ".")
	cmd.Dir = filepath.Join(repoRoot, "cmd", "logreduce")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("build test binary: " + err.Error() + ": " + string(out))
	}
	os.Exit(m.Run())
}

func runCLI(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(testBinary, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run %v: %v (stderr: %s)", args, err, stderr.String())
		}
	}
	return code, stdout.String(), stderr.String()
}

func TestVersionPlainAndJSON(t *testing.T) {
	code, out, _ := runCLI(t, "", "version")
	if code != 0 || strings.TrimSpace(out) != "logreduce 0.1.0" {
		t.Fatalf("version: code=%d out=%q", code, out)
	}
	code, out, _ = runCLI(t, "", "version", "--json")
	if code != 0 {
		t.Fatalf("version --json: code=%d", code)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("version --json is not JSON: %v", err)
	}
	if v["tool"] != "logreduce" || v["version"] != "0.1.0" || v["ok"] != true {
		t.Fatalf("version --json fields wrong: %v", v)
	}
	code, out, _ = runCLI(t, "", "--version")
	if code != 0 || strings.TrimSpace(out) != "logreduce 0.1.0" {
		t.Fatalf("--version: code=%d out=%q", code, out)
	}
}

func TestDoctor(t *testing.T) {
	code, out, stderr := runCLI(t, repoRoot, "doctor")
	if code != 0 {
		t.Fatalf("doctor: code=%d out=%q stderr=%q", code, out, stderr)
	}
	if !strings.Contains(out, "doctor: ok") {
		t.Fatalf("doctor output missing ok: %q", out)
	}
}

func TestSelfTest(t *testing.T) {
	code, out, _ := runCLI(t, repoRoot, "self-test")
	if code != 0 {
		t.Fatalf("self-test: code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "self-test: ok") {
		t.Fatalf("self-test output missing ok: %q", out)
	}
}

func TestReduceVitestE2E(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	logPath := filepath.Join(repoRoot, "fixtures", "vitest", "input.json")
	code, out, stderr := runCLI(t, "", "reduce", "--kind", "vitest", "--log", logPath, "--out", outDir, "--json")
	if code != 1 {
		t.Fatalf("reduce with findings: want exit 1, got %d out=%q", code, out)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("reduce stdout is not JSON: %v", err)
	}
	if resp["ok"] != true || resp["kind"] != "vitest" || resp["version"] != "0.1.0" {
		t.Fatalf("reduce response fields wrong: %v", resp)
	}
	findings, _ := resp["findings"].([]any)
	if len(findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(findings))
	}
	// findings.json must equal stdout (modulo duration_ms).
	written, err := os.ReadFile(filepath.Join(outDir, "findings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wmap map[string]any
	if err := json.Unmarshal(written, &wmap); err != nil {
		t.Fatal(err)
	}
	resp["duration_ms"] = float64(0)
	wmap["duration_ms"] = float64(0)
	if !jsonEqual(resp, wmap) {
		t.Fatal("findings.json differs from stdout")
	}
	// full.log must be a copy of the input.
	orig, _ := os.ReadFile(logPath)
	copied, _ := os.ReadFile(filepath.Join(outDir, "full.log"))
	if string(orig) != string(copied) {
		t.Fatal("full.log is not a copy of the input")
	}
	// Telemetry: one JSON line on stderr, ok=true.
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 telemetry line on stderr, got %d: %q", len(lines), stderr)
	}
	var tele map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &tele); err != nil {
		t.Fatalf("telemetry is not JSON: %v", err)
	}
	if tele["tool"] != "logreduce" || tele["command"] != "reduce" || tele["ok"] != true {
		t.Fatalf("telemetry fields wrong: %v", tele)
	}
}

func jsonEqual(a, b map[string]any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func TestReduceErrorCases(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code string
	}{
		{"missing", []string{"reduce", "--kind", "vitest", "--log", "/nonexistent/x.json", "--out", t.TempDir()}, "INPUT_MISSING"},
		{"unknown-kind", []string{"reduce", "--kind", "nope", "--log", filepath.Join(repoRoot, "fixtures", "vitest", "input.json"), "--out", t.TempDir()}, "UNKNOWN_KIND"},
		{"malformed", nil, "INVALID_INPUT"},
		{"usage", []string{"reduce", "--kind", "vitest"}, "USAGE"},
		{"unknown-command", []string{"bogus"}, "USAGE"},
	}
	for _, c := range cases {
		args := c.args
		if c.name == "malformed" {
			bad := filepath.Join(t.TempDir(), "bad.json")
			if err := os.WriteFile(bad, []byte("not json {{{"), 0o644); err != nil {
				t.Fatal(err)
			}
			args = []string{"reduce", "--kind", "vitest", "--log", bad, "--out", t.TempDir()}
		}
		code, out, _ := runCLI(t, "", args...)
		if code != 2 {
			t.Fatalf("%s: want exit 2, got %d out=%q", c.name, code, out)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			t.Fatalf("%s: stdout is not JSON: %v", c.name, err)
		}
		if resp["ok"] != false {
			t.Fatalf("%s: ok should be false: %v", c.name, resp)
		}
		errObj, _ := resp["error"].(map[string]any)
		if errObj["code"] != c.code {
			t.Fatalf("%s: want code %s, got %v", c.name, c.code, errObj["code"])
		}
		for _, field := range []string{"blocked", "fix", "if_that_fails"} {
			if s, _ := errObj[field].(string); s == "" {
				t.Fatalf("%s: error.%s is empty", c.name, field)
			}
		}
	}
}

func TestReduceRequestEnvelope(t *testing.T) {
	req := `{"protocol":"arabica-plugin-v1","command":"reduce","args":{"kind":"vitest","log":"` +
		filepath.Join(repoRoot, "fixtures", "vitest", "input.json") +
		`","out":"` + filepath.Join(t.TempDir(), "out") + `"}}`
	reqPath := filepath.Join(t.TempDir(), "req.json")
	if err := os.WriteFile(reqPath, []byte(req), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCLI(t, "", "reduce", "--request", reqPath)
	if code != 1 {
		t.Fatalf("envelope reduce: want exit 1, got %d out=%q", code, out)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp["findings"].([]any)) != 2 {
		t.Fatalf("envelope reduce: want 2 findings, got %v", resp["findings"])
	}
}

func TestHelp(t *testing.T) {
	code, out, _ := runCLI(t, "", "--help")
	if code != 0 || !strings.Contains(out, "reduce --kind") {
		t.Fatalf("--help: code=%d out=%q", code, out)
	}
}

// captureOutput runs fn with stdout/stderr captured and returns its
// exit code plus what it wrote. os.Args is also swapped for run().
func captureOutput(t *testing.T, args []string, fn func() int) (int, string, string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	oldArgs := os.Args
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW
	os.Args = append([]string{"logreduce"}, args...)
	code := fn()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	os.Args = oldArgs
	outBytes := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := outR.Read(buf)
		outBytes = append(outBytes, buf[:n]...)
		if err != nil {
			break
		}
	}
	errBytes := make([]byte, 0, 4096)
	for {
		n, err := errR.Read(buf)
		errBytes = append(errBytes, buf[:n]...)
		if err != nil {
			break
		}
	}
	return code, string(outBytes), string(errBytes)
}

func captureRun(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	return captureOutput(t, args, func() int { return run(time.Now()) })
}

// TestCmdReduceInProcess exercises cmdReduce directly so its branches
// count toward cmd/ coverage (the subprocess e2e tests above cover the
// same behaviour, but go cover cannot see inside a subprocess).
func TestParseFlags(t *testing.T) {
	fs, err := parseFlags([]string{"--kind", "vitest", "--json"})
	if err != nil || fs.vals["kind"] != "vitest" || !fs.bools["json"] {
		t.Fatalf("parseFlags basic: fs=%v err=%v", fs, err)
	}
	fs, err = parseFlags([]string{"--kind=vitest", "extra"})
	if err != nil || fs.vals["kind"] != "vitest" || len(fs.rest) != 1 {
		t.Fatalf("parseFlags = form: fs=%v err=%v", fs, err)
	}
	if _, err := parseFlags([]string{"--bogus"}); err == nil {
		t.Fatal("parseFlags unknown flag should error")
	}
	if _, err := parseFlags([]string{"--kind"}); err == nil {
		t.Fatal("parseFlags missing value should error")
	}
}

func TestCmdReduceEnvelopeBranches(t *testing.T) {
	writeReq := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "req.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Wrong protocol.
	req := writeReq(t, `{"protocol":"other-v9","command":"reduce","args":{"kind":"vitest","log":"x","out":"y"}}`)
	code, out, _ := captureOutput(t, nil, func() int { return cmdReduce([]string{"--request", req}, time.Now()) })
	if code != 2 || !strings.Contains(out, "INVALID_REQUEST") {
		t.Fatalf("bad protocol: code=%d out=%q", code, out)
	}
	// Wrong command.
	req = writeReq(t, `{"protocol":"arabica-plugin-v1","command":"doctor","args":{"kind":"vitest","log":"x","out":"y"}}`)
	code, out, _ = captureOutput(t, nil, func() int { return cmdReduce([]string{"--request", req}, time.Now()) })
	if code != 2 || !strings.Contains(out, "INVALID_REQUEST") {
		t.Fatalf("bad command: code=%d out=%q", code, out)
	}
	// Extra positional argument.
	code, out, _ = captureOutput(t, nil, func() int {
		return cmdReduce([]string{"--kind", "vitest", "--log", filepath.Join(repoRoot, "fixtures", "vitest", "input.json"), "--out", t.TempDir(), "extra"}, time.Now())
	})
	if code != 2 || !strings.Contains(out, "USAGE") {
		t.Fatalf("extra arg: code=%d out=%q", code, out)
	}
	// Missing request file.
	code, out, _ = captureOutput(t, nil, func() int { return cmdReduce([]string{"--request", "/nonexistent/req.json"}, time.Now()) })
	if code != 2 || !strings.Contains(out, "INPUT_MISSING") {
		t.Fatalf("missing request: code=%d out=%q", code, out)
	}
}

func TestCmdReduceInProcess(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	code, out, _ := captureOutput(t, nil, func() int {
		return cmdReduce([]string{"--kind", "vitest", "--log", filepath.Join(repoRoot, "fixtures", "vitest", "input.json"), "--out", outDir, "--json"}, time.Now())
	})
	if code != 1 {
		t.Fatalf("cmdReduce vitest: want exit 1, got %d out=%q", code, out)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp["findings"].([]any)) != 2 {
		t.Fatalf("cmdReduce vitest: want 2 findings, got %v", resp["findings"])
	}
	code, out, _ = captureOutput(t, nil, func() int {
		return cmdReduce([]string{"--kind", "vitest", "--log", "/nonexistent.json", "--out", t.TempDir()}, time.Now())
	})
	if code != 2 || !strings.Contains(out, "INPUT_MISSING") {
		t.Fatalf("cmdReduce missing: code=%d out=%q", code, out)
	}
	code, out, _ = captureOutput(t, nil, func() int {
		return cmdReduce([]string{"--kind", "vitest"}, time.Now())
	})
	if code != 2 || !strings.Contains(out, "USAGE") {
		t.Fatalf("cmdReduce usage: code=%d out=%q", code, out)
	}
}

func TestCmdVersionDoctorSelfTestInProcess(t *testing.T) {
	code, out, _ := captureOutput(t, nil, func() int { return cmdVersion(nil, time.Now()) })
	if code != 0 || strings.TrimSpace(out) != "logreduce 0.1.0" {
		t.Fatalf("cmdVersion: code=%d out=%q", code, out)
	}
	code, out, _ = captureOutput(t, nil, func() int { return cmdDoctor(nil, time.Now()) })
	if code != 0 || !strings.Contains(out, "doctor: ok") {
		t.Fatalf("cmdDoctor: code=%d out=%q", code, out)
	}
	// self-test reads fixtures/ relative to the working directory.
	oldWd, _ := os.Getwd()
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	code, out, _ = captureOutput(t, nil, func() int { return cmdSelfTest(nil, time.Now()) })
	if code != 0 || !strings.Contains(out, "self-test: ok") {
		t.Fatalf("cmdSelfTest: code=%d out=%q", code, out)
	}
}

func TestRunDispatch(t *testing.T) {
	code, out, _ := captureRun(t, []string{"version"})
	if code != 0 || strings.TrimSpace(out) != "logreduce 0.1.0" {
		t.Fatalf("run version: code=%d out=%q", code, out)
	}
	code, out, _ = captureRun(t, []string{"--help"})
	if code != 0 || !strings.Contains(out, "Usage:") {
		t.Fatalf("run --help: code=%d out=%q", code, out)
	}
	code, out, _ = captureRun(t, []string{"bogus"})
	if code != 2 {
		t.Fatalf("run bogus: want exit 2, got %d out=%q", code, out)
	}
	code, out, _ = captureRun(t, []string{})
	if code != 2 || !strings.Contains(out, `"code":"USAGE"`) {
		t.Fatalf("run with no args: code=%d out=%q", code, out)
	}
}

func TestWithTimeout(t *testing.T) {
	if err := withTimeout(time.Second, func() error { return nil }); err != nil {
		t.Fatalf("fast function should not time out: %v", err)
	}
	slow := func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	}
	if err := withTimeout(time.Millisecond, slow); err != errTimeout {
		t.Fatalf("slow function with 1ms timeout: want errTimeout, got %v", err)
	}
}
