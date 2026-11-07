package reduce

import (
	"strings"
	"testing"
)

// BenchmarkGeneric11MB measures the generic parser on an 11 MB log
// built in memory (the acceptance case: a 10 MB log must finish in
// under 2 seconds). Run with: go test -run '^$' -bench . ./internal/reduce/
func BenchmarkGeneric11MB(b *testing.B) {
	var sb strings.Builder
	chunk := "2026-01-01 build started\nsrc/main.go:42: error: undefined variable foo\nall good here\npanic: runtime error: index out of range\n  goroutine 1 [running]:\n"
	for sb.Len() < 11*1024*1024 {
		sb.WriteString(chunk)
	}
	data := []byte(sb.String())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := parseGeneric(data)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.findings) == 0 {
			b.Fatal("no findings")
		}
	}
}

func BenchmarkTSC11MB(b *testing.B) {
	var sb strings.Builder
	chunk := "src/app.ts(10,5): error TS2322: Type 'string' is not assignable to type 'number'.\nsrc/app.ts(20,3): error TS2304: Cannot find name 'foo'.\n"
	for sb.Len() < 11*1024*1024 {
		sb.WriteString(chunk)
	}
	data := []byte(sb.String())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := parseTSC(data)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.findings) == 0 {
			b.Fatal("no findings")
		}
	}
}
