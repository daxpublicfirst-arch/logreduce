package reduce

import (
	"strconv"
	"strings"
)

// generic parser: text lines containing error / failed / failure /
// exception / panic (as whole words). Keeps path:line when present;
// context capped at 5 lines (the matching line plus the next 4).
//
// No regular expressions are used here: the generic parser runs on
// every line of the log, and Go's regexp backtracker made a 10 MB log
// take ~1.8s of CPU. Hand-written scanning is ~10x faster and keeps
// the same matching semantics (whole-word keywords, path:line with a
// dotted extension or a slash in the path).

var failureWords = [...]string{"error", "failed", "failure", "exception", "panic"}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// containsFailureWord reports whether s contains one of the failure
// keywords as a whole word, case-insensitively.
func containsFailureWord(s string) bool {
	for i := 0; i < len(s); {
		if !isWordByte(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isWordByte(s[j]) {
			j++
		}
		word := s[i:j]
		for _, kw := range failureWords {
			if len(word) == len(kw) {
				match := true
				for k := 0; k < len(word); k++ {
					if lowerByte(word[k]) != kw[k] {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
		i = j
	}
	return false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// parseFileLineToken tries to read a `path:line` or `path:line:col`
// token. The path must contain a '.' (a file extension) or a '/' or
// a '\', so timestamps like 12:34 do not match. Leading/trailing
// punctuation such as ( ) [ ] , : is stripped first.
func parseFileLineToken(tok string) (string, int, bool) {
	tok = strings.Trim(tok, "()[],\"'`")
	tok = strings.TrimSuffix(tok, ":")
	if tok == "" {
		return "", 0, false
	}
	colon := strings.IndexByte(tok, ':')
	if colon <= 0 || colon+1 >= len(tok) {
		return "", 0, false
	}
	path := tok[:colon]
	rest := tok[colon+1:]
	// Line number: leading digits of rest.
	k := 0
	for k < len(rest) && isDigit(rest[k]) {
		k++
	}
	if k == 0 {
		return "", 0, false
	}
	// After the digits only ':' (for :col) or end of token is allowed.
	if k < len(rest) && rest[k] != ':' {
		return "", 0, false
	}
	if !strings.ContainsAny(path, "./\\") {
		return "", 0, false
	}
	// A dotted path must have an extension-like tail: a '.' followed
	// by at least one alnum char.
	if strings.Contains(path, ".") {
		dot := strings.LastIndexByte(path, '.')
		if dot == len(path)-1 {
			return "", 0, false
		}
	}
	n, err := strconv.Atoi(rest[:k])
	if err != nil {
		return "", 0, false
	}
	return path, n, true
}

// extractFileLine finds the first path:line token in a line.
func extractFileLine(line string) (string, int) {
	for _, tok := range strings.Fields(line) {
		if f, n, ok := parseFileLineToken(tok); ok {
			return f, n
		}
	}
	return "", 0
}

func parseGeneric(data []byte) (*parseResult, error) {
	res := &parseResult{}
	lines := splitLines(data)
	for i, raw := range lines {
		line := stripANSI(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if !containsFailureWord(line) {
			continue
		}
		file, ln := extractFileLine(line)
		// Context: the line itself plus up to 4 following lines.
		var ctx []string
		ctx = append(ctx, strings.TrimSpace(line))
		for j := i + 1; j < len(lines) && len(ctx) < 5; j++ {
			l := stripANSI(strings.TrimSpace(lines[j]))
			if l == "" {
				continue
			}
			ctx = append(ctx, l)
		}
		res.findings = append(res.findings, Finding{
			File: file, Line: ln, Severity: "error",
			Message: truncateMsg(strings.TrimSpace(line), 500),
			Context: ctx,
		})
	}
	return res, nil
}
