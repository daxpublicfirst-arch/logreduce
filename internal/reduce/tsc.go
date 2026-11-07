package reduce

import (
	"regexp"
	"strconv"
	"strings"
)

// tsc parser: lines of `file(line,col): error TSxxxx: message`
// (and `warning TSxxxx:`).

var tscRe = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\): (error|warning) (TS\d+: .+)$`)
var tscLooseRe = regexp.MustCompile(`^(.+?):(\d+):(\d+): (error|warning) (TS\d+: .+)$`)

func parseTSC(data []byte) (*parseResult, error) {
	res := &parseResult{}
	lines := splitLines(data)
	sawAny := false
	for i, raw := range lines {
		line := stripANSI(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if m := tscRe.FindStringSubmatch(line); m != nil {
			sawAny = true
			n, _ := strconv.Atoi(m[2])
			sev := "error"
			if m[4] == "warning" {
				sev = "warning"
			}
			ctx := nextLines(lines, i, 2)
			res.findings = append(res.findings, Finding{
				File: m[1], Line: n, Severity: sev,
				Message: truncateMsg(m[5], 500), Context: ctx,
			})
			continue
		}
		if m := tscLooseRe.FindStringSubmatch(line); m != nil {
			sawAny = true
			n, _ := strconv.Atoi(m[2])
			sev := "error"
			if m[4] == "warning" {
				sev = "warning"
			}
			ctx := nextLines(lines, i, 2)
			res.findings = append(res.findings, Finding{
				File: m[1], Line: n, Severity: sev,
				Message: truncateMsg(m[5], 500), Context: ctx,
			})
			continue
		}
		// A line that looks like a tsc failure but does not parse.
		if strings.Contains(line, "error TS") || strings.Contains(line, "warning TS") {
			res.unparsed++
			addUnparsed(res, line)
		}
	}
	if !sawAny && res.unparsed == 0 {
		// No tsc diagnostics at all: valid input, zero findings.
		// (tsc with no errors prints nothing.)
	}
	return res, nil
}

// nextLines returns up to n following non-empty lines as context.
func nextLines(lines []string, i, n int) []string {
	var out []string
	for j := i + 1; j < len(lines) && len(out) < n; j++ {
		l := stripANSI(strings.TrimSpace(lines[j]))
		if l == "" {
			continue
		}
		if tscRe.MatchString(l) || tscLooseRe.MatchString(l) {
			break
		}
		out = append(out, l)
	}
	return out
}

// addUnparsed keeps up to MaxUnparsedFindings raw findings for lines
// that looked like failures but could not be parsed.
func addUnparsed(res *parseResult, line string) {
	if len(res.findings) >= MaxUnparsedFindings && res.unparsed > MaxUnparsedFindings {
		return
	}
	// Only add a raw finding while we are under the cap; the count
	// in res.unparsed keeps growing regardless.
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
