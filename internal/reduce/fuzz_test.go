package reduce

import (
	"os"
	"testing"
)

// Fuzz tests: parsers must never panic on arbitrary bytes. They may
// return an error, but they must not crash, hang, or leak. Seed corpus
// is the real fixture input for each kind. Run with:
// go test -run='^$' -fuzz=FuzzParseGeneric -fuzztime=30s ./internal/reduce/

func fuzzParse(t *testing.T, kind string, data []byte) {
	t.Helper()
	if _, err := parse(kind, data); err != nil {
		if _, ok := err.(*ToolError); !ok {
			t.Fatalf("%s: unexpected error type %T: %v", kind, err, err)
		}
	}
}

func FuzzParseGeneric(f *testing.F) {
	seed, _ := os.ReadFile("../../fixtures/generic/input.log")
	f.Add(seed)
	f.Add([]byte("error: boom\n"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) { fuzzParse(t, "generic", data) })
}

func FuzzParseTSC(f *testing.F) {
	seed, _ := os.ReadFile("../../fixtures/tsc/input.txt")
	f.Add(seed)
	f.Add([]byte("src/a.ts(1,2): error TS1: x\n"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) { fuzzParse(t, "tsc", data) })
}

func FuzzParseVitest(f *testing.F) {
	seed, _ := os.ReadFile("../../fixtures/vitest/input.json")
	f.Add(seed)
	f.Add([]byte("{}"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) { fuzzParse(t, "vitest", data) })
}

func FuzzParsePlaywright(f *testing.F) {
	seed, _ := os.ReadFile("../../fixtures/playwright/input.json")
	f.Add(seed)
	f.Add([]byte("{}"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) { fuzzParse(t, "playwright", data) })
}

func FuzzParseESLint(f *testing.F) {
	seed, _ := os.ReadFile("../../fixtures/eslint/input.json")
	f.Add(seed)
	f.Add([]byte("[]"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) { fuzzParse(t, "eslint", data) })
}

func FuzzParseTRX(f *testing.F) {
	seed, _ := os.ReadFile("../../fixtures/trx/input.trx")
	f.Add(seed)
	f.Add([]byte("<TestRun></TestRun>"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) { fuzzParse(t, "trx", data) })
}

func FuzzParsePytest(f *testing.F) {
	seed, _ := os.ReadFile("../../fixtures/pytest/input.json")
	f.Add(seed)
	f.Add([]byte("{}"))
	f.Add([]byte("<testsuites></testsuites>"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) { fuzzParse(t, "pytest", data) })
}

func FuzzParseGoTest(f *testing.F) {
	seed, _ := os.ReadFile("../../fixtures/go-test/input.txt")
	f.Add(seed)
	f.Add([]byte("{\"Action\":\"fail\",\"Test\":\"T\"}\n"))
	f.Add([]byte(""))
	f.Fuzz(func(t *testing.T, data []byte) { fuzzParse(t, "go-test", data) })
}
