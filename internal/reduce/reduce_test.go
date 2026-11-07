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

func TestTSCLooseForm(t *testing.T) {
	res, err := parseTSC([]byte("src/a.ts:12:3: error TS2322: Type 'string' is not assignable to type 'number'.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(res.findings))
	}
	f := res.findings[0]
	if f.File != "src/a.ts" || f.Line != 12 || f.Severity != "error" || f.Message != "TS2322: Type 'string' is not assignable to type 'number'." {
		t.Fatalf("loose form wrong: %+v", f)
	}
}

func TestGenericWordBoundaries(t *testing.T) {
	// "terror" contains "error" but is not a failure word; "ERROR"
	// in capitals is one. A timestamp (12:34) is not a file:line.
	res, err := parseGeneric([]byte("the terror of it all\n12:34 ERROR: boom\nsrc/x.go:9: failed to start\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.findings) != 2 {
		t.Fatalf("want 2 findings, got %d: %+v", len(res.findings), res.findings)
	}
	if res.findings[0].File != "" || res.findings[0].Line != 0 {
		t.Fatalf("timestamp line should have no file:line: %+v", res.findings[0])
	}
	if res.findings[1].File != "src/x.go" || res.findings[1].Line != 9 {
		t.Fatalf("file:line wrong: %+v", res.findings[1])
	}
}

func TestStripANSI(t *testing.T) {
	if got := stripANSI("plain line"); got != "plain line" {
		t.Fatalf("plain line changed: %q", got)
	}
	if got := stripANSI("\x1b[31mred\x1b[0m text"); got != "red text" {
		t.Fatalf("ANSI not stripped: %q", got)
	}
	if got := stripANSI("\x1b[1;32mPASS\x1b[0m"); got != "PASS" {
		t.Fatalf("ANSI codes not stripped: %q", got)
	}
}

func TestTRXFileLine(t *testing.T) {
	file, line := trxFileLine("   at Calc.Divides() in C:\\work\\calc\\CalculatorTests.cs:line 42\n")
	if file != "C:/work/calc/CalculatorTests.cs" || line != 42 {
		t.Fatalf("drive path wrong: %s:%d", file, line)
	}
	file, line = trxFileLine("   at Tests.Run() in /home/u/tests.cs:line 7\n")
	if file != "/home/u/tests.cs" || line != 7 {
		t.Fatalf("unix path wrong: %s:%d", file, line)
	}
	file, line = trxFileLine("no stack frame here\n")
	if file != "" || line != 0 {
		t.Fatalf("expected no match, got %s:%d", file, line)
	}
}

func TestFileLineFromText(t *testing.T) {
	file, line := fileLineFromText("AssertionError: x\n    at src/a.test.ts:12:15\n")
	if file != "src/a.test.ts" || line != 12 {
		t.Fatalf("wrong: %s:%d", file, line)
	}
	file, line = fileLineFromText("nothing here\n")
	if file != "" || line != 0 {
		t.Fatalf("expected no match, got %s:%d", file, line)
	}
}

func TestReduceFindingsCap(t *testing.T) {
	// 1500 failure lines: findings must cap at 1000, truncated true.
	var sb strings.Builder
	for i := 0; i < 1500; i++ {
		sb.WriteString("src/f.go:1: error: boom\n")
	}
	logPath := filepath.Join(t.TempDir(), "big.log")
	if err := os.WriteFile(logPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, _, err := Reduce("generic", logPath, filepath.Join(t.TempDir(), "out"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Findings) != MaxFindings {
		t.Fatalf("want %d findings, got %d", MaxFindings, len(resp.Findings))
	}
	if !resp.Truncated {
		t.Fatal("want truncated=true when findings are capped")
	}
}

func TestReduceContextCap(t *testing.T) {
	// One vitest failure with a 12-line failure message: context must
	// be cut to MaxContextLines by Reduce.
	input := `{"numTotalTests":1,"numPassedTests":0,"numFailedTests":1,"testResults":[{"name":"src/a.test.ts","assertionResults":[{"title":"t","fullName":"t","status":"failed","failureMessages":["line0\nline1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11"],"location":{"line":1,"column":1}}]}]}`
	logPath := filepath.Join(t.TempDir(), "in.json")
	if err := os.WriteFile(logPath, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, _, err := Reduce("vitest", logPath, filepath.Join(t.TempDir(), "out"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Findings) != 1 || len(resp.Findings[0].Context) != MaxContextLines {
		t.Fatalf("want context capped at %d, got %+v", MaxContextLines, resp.Findings)
	}
	if !resp.Truncated {
		t.Fatal("want truncated=true when context is cut")
	}
}

func TestReduceInputLimitTruncation(t *testing.T) {
	old := MaxInputBytes
	MaxInputBytes = 100
	t.Cleanup(func() { MaxInputBytes = old })
	logPath := filepath.Join(t.TempDir(), "in.log")
	content := strings.Repeat("error: boom at src/a.go:1\n", 20) // > 100 bytes
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, _, err := Reduce("generic", logPath, filepath.Join(t.TempDir(), "out"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated {
		t.Fatal("want truncated=true when input exceeds the limit")
	}
}

// TestRealWorldFixtures runs the parsers on logs produced by real
// runners (not hand-written): vitest/tsc/eslint from a real Node
// project, pytest (JSON report + JUnit XML) from a real pytest run,
// and go test -json from a real Go module. See docs/REAL_WORLD.md for
// how they were generated. Paths were relativised (the machine prefix
// replaced with "proj") so the fixtures are portable.
func TestPytestLongreprForms(t *testing.T) {
	// longrepr as a plain string.
	res, err := parsePytest([]byte(`{"tests":[{"nodeid":"a.py::t1","outcome":"failed","call":{"longrepr":"AssertionError: boom\n"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.findings) != 1 || res.findings[0].Message != "AssertionError: boom" {
		t.Fatalf("string longrepr wrong: %+v", res.findings)
	}
	// longrepr as a dict with reprcrash.
	res, err = parsePytest([]byte(`{"tests":[{"nodeid":"a.py::t2","outcome":"failed","call":{"longrepr":{"reprcrash":{"path":"a.py","lineno":9,"message":"ValueError: bad"}}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.findings) != 1 || res.findings[0].Line != 9 || !strings.Contains(res.findings[0].Message, "ValueError") {
		t.Fatalf("dict longrepr wrong: %+v", res.findings)
	}
	// longrepr as a list of lines.
	res, err = parsePytest([]byte(`{"tests":[{"nodeid":"a.py::t3","outcome":"failed","call":{"longrepr":["line one","line two"]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.findings) != 1 || res.findings[0].Message != "line one" {
		t.Fatalf("list longrepr wrong: %+v", res.findings)
	}
}

func TestRealWorldFixtures(t *testing.T) {
	cases := []struct {
		kind string
		file string
		want []struct {
			file string
			line int
		}
	}{
		{"vitest", "../../fixtures-real/vitest/input.json", []struct {
			file string
			line int
		}{{"proj/math.test.ts", 4}, {"proj/math.test.ts", 5}}},
		{"tsc", "../../fixtures-real/tsc/input.txt", []struct {
			file string
			line int
		}{{"app.ts", 1}, {"app.ts", 2}}},
		{"eslint", "../../fixtures-real/eslint/input.json", []struct {
			file string
			line int
		}{{"proj/app.js", 1}, {"proj/app.js", 2}, {"proj/app.js", 3}}},
		{"pytest", "../../fixtures-real/pytest/input.json", []struct {
			file string
			line int
		}{{"test_math.py", 5}}},
		{"pytest", "../../fixtures-real/pytest/input-junit.xml", []struct {
			file string
			line int
		}{{"test_math.py", 5}}},
		{"go-test", "../../fixtures-real/go-test/input.txt", []struct {
			file string
			line int
		}{{"main_test.go", 7}}},
	}
	for _, c := range cases {
		data, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("%s %s: %v", c.kind, c.file, err)
		}
		res, err := parse(c.kind, data)
		if err != nil {
			t.Fatalf("%s %s: parse error: %v", c.kind, c.file, err)
		}
		if len(res.findings) != len(c.want) {
			t.Fatalf("%s %s: want %d findings, got %d: %+v", c.kind, c.file, len(c.want), len(res.findings), res.findings)
		}
		for i, w := range c.want {
			if res.findings[i].File != w.file || res.findings[i].Line != w.line {
				t.Fatalf("%s %s finding %d: want %s:%d, got %s:%d", c.kind, c.file, i, w.file, w.line, res.findings[i].File, res.findings[i].Line)
			}
		}
	}
}

func TestReduceUnparsedCap(t *testing.T) {
	// 60 unparseable tsc-looking lines: unparsed counts all 60, but
	// at most MaxUnparsedFindings raw findings are kept.
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("weird error TS line without parens\n")
	}
	res, err := parseTSC([]byte(sb.String()))
	if err != nil {
		t.Fatal(err)
	}
	if res.unparsed != 60 {
		t.Fatalf("want unparsed=60, got %d", res.unparsed)
	}
	raw := 0
	for _, f := range res.findings {
		if f.File == "" && f.Line == 0 {
			raw++
		}
	}
	if raw != MaxUnparsedFindings {
		t.Fatalf("want %d raw findings, got %d", MaxUnparsedFindings, raw)
	}
}
