package reduce

import (
	"strconv"
	"strings"
)

// tsc parser: lines of `file(line,col): error TSxxxx: message`
// (and `warning TSxxxx:`), plus the loose `file:line:col: error TS…`
// form. Parsed by hand, not with regexps: tsc logs can be many
// megabytes, and regexp backtracking is the slowest part of the tool.

// parseTSCLine parses one tsc diagnostic line. It returns
// file, line, severity ("error"/"warning"), message, ok.
func parseTSCLine(line string) (string, int, string, string, bool) {
	// Form 1: file(line,col): error TSxxxx: message
	if idx := strings.IndexByte(line, '('); idx > 0 {
		rest := line[idx+1:]
		comma := strings.IndexByte(rest, ',')
		if comma > 0 {
			num := rest[:comma]
			after := rest[comma+1:]
			closeIdx := strings.Index(after, "): ")
			if closeIdx > 0 {
				colPart := after[:closeIdx]
				if allDigits(num) && allDigits(colPart) {
					sevMsg := after[closeIdx+3:]
					if sev, msg, ok := splitTSCSeverity(sevMsg); ok {
						n, _ := strconv.Atoi(num)
						return line[:idx], n, sev, msg, true
					}
				}
			}
		}
	}
	// Form 2: file:line:col: error TSxxxx: message
	parts := strings.SplitN(line, ":", 4)
	if len(parts) == 4 && allDigits(strings.TrimSpace(parts[1])) && allDigits(strings.TrimSpace(parts[2])) {
		if sev, msg, ok := splitTSCSeverity(strings.TrimSpace(parts[3])); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			return strings.TrimSpace(parts[0]), n, sev, msg, true
		}
	}
	return "", 0, "", "", false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// splitTSCSeverity splits "error TS1234: ..." / "warning TS1234: ...".
func splitTSCSeverity(s string) (string, string, bool) {
	for _, sev := range []string{"error", "warning"} {
		if strings.HasPrefix(s, sev+" TS") {
			msg := strings.TrimPrefix(s, sev+" ")
			if strings.HasPrefix(msg, "TS") {
				if sev == "warning" {
					return "warning", msg, true
				}
				return "error", msg, true
			}
		}
	}
	return "", "", false
}

func parseTSC(data []byte) (*parseResult, error) {
	res := &parseResult{}
	lines := splitLines(data)
	for i, raw := range lines {
		line := stripANSI(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if file, n, sev, msg, ok := parseTSCLine(line); ok {
			ctx := nextLines(lines, i, 2)
			res.findings = append(res.findings, Finding{
				File: file, Line: n, Severity: sev,
				Message: truncateMsg(msg, 500), Context: ctx,
			})
			continue
		}
		// A line that looks like a tsc failure but does not parse.
		if strings.Contains(line, "error TS") || strings.Contains(line, "warning TS") {
			res.unparsed++
			addUnparsed(res, line)
		}
	}
	return res, nil
}

// nextLines returns up to n following non-empty lines as context,
// stopping at the next diagnostic line.
func nextLines(lines []string, i, n int) []string {
	var out []string
	for j := i + 1; j < len(lines) && len(out) < n; j++ {
		l := stripANSI(strings.TrimSpace(lines[j]))
		if l == "" {
			continue
		}
		if _, _, _, _, ok := parseTSCLine(l); ok {
			break
		}
		out = append(out, l)
	}
	return out
}

// addUnparsed keeps up to MaxUnparsedFindings raw findings for lines
// that looked like failures but could not be parsed.
func addUnparsed(res *parseResult, line string) {
	count := 0
	for _, f := range res.findings {
		if f.File == "" && f.Line == 0 {
			count++
		}
	}
	if count >= MaxUnparsedFindings {
		return
	}
	res.findings = append(res.findings, Finding{
		File: "", Line: 0, Severity: "error",
		Message: truncateMsg(stripANSI(strings.TrimSpace(line)), 500),
		Context: []string{stripANSI(strings.TrimSpace(line))},
	})
}
