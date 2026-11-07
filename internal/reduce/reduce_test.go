package reduce

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func TestVitestTwoFailures(t *testing.T) {
	res, err := parseVitest(mustRead(t, "../../fixtures/vitest/input.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.findings) != 2 {
		t.Fatalf("want 2 findings, got %d", len(res.findings))
	}
	if res.summary.Failed != 2 || res.summary.Total != 3 || res.summary.Passed != 1 {
		t.Fatalf("bad summary: %+v", res.summary)
	}
	// Sorted by file in Reduce; parser order is a.test.ts then b.test.ts.
	if res.findings[0].File != "src/a.test.ts" || res.findings[0].Line != 12 {
		t.Fatalf("first finding wrong: %+v", res.findings[0])
	}
	if res.findings[0].Message != "AssertionError: expected 1 to be 2" {
		t.Fatalf("first message wrong: %q", res.findings[0].Message)
	}
	if res.findings[1].File != "src/b.test.ts" || res.findings[1].Line != 7 {
		t.Fatalf("second finding wrong: %+v", res.findings[1])
	}
}

func TestTRXOneFailure(t *testing.T) {
	res, err := parseTRX(mustRead(t, "../../fixtures/trx/input.trx"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(res.findings))
	}
	f := res.findings[0]
	if f.Test != "CalculatorTests.Divides" {
		t.Fatalf("test name wrong: %q", f.Test)
	}
	if f.Line != 42 || !strings.Contains(f.File, "CalculatorTests.cs") {
		t.Fatalf("file:line wrong: %s:%d", f.File, f.Line)
	}
	if res.summary.Total != 2 || res.summary.Failed != 1 {
		t.Fatalf("bad summary: %+v", res.summary)
	}
}

func TestPlaywrightTSCESLintPytestGoTestGeneric(t *testing.T) {
	cases := []struct {
		kind string
		path string
		want int
	}{
		{"playwright", "../../fixtures/playwright/input.json", 1},
		{"tsc", "../../fixtures/tsc/input.txt", 3},
		{"eslint", "../../fixtures/eslint/input.json", 2},
		{"pytest", "../../fixtures/pytest/input.json", 1},
		{"go-test", "../../fixtures/go-test/input.txt", 1},
		{"generic", "../../fixtures/generic/input.log", 2},
	}
	for _, c := range cases {
		res, err := parse(c.kind, mustRead(t, c.path))
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if len(res.findings) != c.want {
			t.Fatalf("%s: want %d findings, got %d: %+v", c.kind, c.want, len(res.findings), res.findings)
		}
	}
}

func TestPytestFileLine(t *testing.T) {
	res, err := parsePytest(mustRead(t, "../../fixtures/pytest/input.json"))
	if err != nil {
		t.Fatal(err)
	}
	if res.findings[0].File != "tests/test_math.py" || res.findings[0].Line != 14 {
		t.Fatalf("pytest file:line wrong: %+v", res.findings[0])
	}
}

func TestGoTestFileLine(t *testing.T) {
	res, err := parseGoTest(mustRead(t, "../../fixtures/go-test/input.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if res.findings[0].File != "main_test.go" || res.findings[0].Line != 10 {
		t.Fatalf("go-test file:line wrong: %+v", res.findings[0])
	}
	if res.findings[0].Test != "TestAdd" {
		t.Fatalf("go-test test wrong: %q", res.findings[0].Test)
	}
}

func TestMalformedInputs(t *testing.T) {
	for _, kind := range []string{"vitest", "playwright", "eslint", "pytest", "trx"} {
		if _, err := parse(kind, []byte("not json at all {{{")); err == nil {
			t.Fatalf("%s: expected error for malformed input", kind)
		} else if te, ok := err.(*ToolError); !ok || te.Exit != 2 {
			t.Fatalf("%s: expected exit-2 ToolError, got %T %v", kind, err, err)
		}
	}
	if _, err := parse("go-test", []byte("plain text, no json lines\n")); err == nil {
		t.Fatal("go-test: expected error for non-JSON input")
	}
}

func TestUnknownKind(t *testing.T) {
	if _, err := parse("nope", []byte("x")); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestReduceWritesOnlyUnderOut(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")
	_, out, err := Reduce("vitest", "../../fixtures/vitest/input.json", outDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("no stdout bytes")
	}
	for _, name := range []string{"full.log", "findings.json"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
	}
	// Nothing else was written under dir.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "out" {
		t.Fatalf("unexpected files under %s: %v", dir, entries)
	}
	// findings.json equals stdout.
	written, _ := os.ReadFile(filepath.Join(outDir, "findings.json"))
	if string(written) != string(out) {
		t.Fatal("findings.json differs from stdout")
	}
}

func TestReduceMissingInput(t *testing.T) {
	_, _, err := Reduce("vitest", "/nonexistent/log.json", t.TempDir(), time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
	te, ok := err.(*ToolError)
	if !ok || te.Code != "INPUT_MISSING" || te.Exit != 2 {
		t.Fatalf("wrong error: %T %v", err, err)
	}
}

func TestSortingDeterministic(t *testing.T) {
	fs := []Finding{
		{File: "b.ts", Line: 1, Severity: "error", Message: "z"},
		{File: "a.ts", Line: 9, Severity: "error", Message: "a"},
		{File: "a.ts", Line: 2, Severity: "error", Message: "m"},
	}
	sortFindings(fs)
	if fs[0].File != "a.ts" || fs[0].Line != 2 || fs[1].Line != 9 || fs[2].File != "b.ts" {
		t.Fatalf("sorting wrong: %+v", fs)
	}
}

func TestTSCUnparsed(t *testing.T) {
	res, err := parseTSC([]byte("weird error TS line without parens\nsrc/a.ts(1,1): error TS1000: bad\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.unparsed != 1 {
		t.Fatalf("want unparsed=1, got %d", res.unparsed)
	}
	if len(res.findings) != 2 {
		t.Fatalf("want 2 findings (1 parsed + 1 raw), got %d", len(res.findings))
	}
}
