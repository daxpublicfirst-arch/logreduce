package reduce

import (
	"regexp"
	"strconv"
	"strings"
)

// generic parser: text lines containing error / failed / failure /
// exception / panic. Keeps path:line when present; context capped at
// 5 lines (the matching line plus the next 4).

var genericNeedleRe = regexp.MustCompile(`(?i)\b(error|failed|failure|exception|panic)\b`)
var genericFileLineRe = regexp.MustCompile(`([^\s:'"]+\.[a-zA-Z0-9]+):(\d+):?`)
var genericFileLineRe2 = regexp.MustCompile(`([^\s:'"]+):(\d+):\d+`)

func parseGeneric(data []byte) (*parseResult, error) {
	res := &parseResult{}
	lines := splitLines(data)
	for i, raw := range lines {
		line := stripANSI(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if !genericNeedleRe.MatchString(line) {
			continue
		}
		file, ln := "", 0
		if m := genericFileLineRe.FindStringSubmatch(line); m != nil {
			// Avoid treating a timestamp like 12:34 as file:line and
			// avoid picking up a bare word with a colon; require a
			// dot in the file part, which the regex already does.
			n, _ := strconv.Atoi(m[2])
			file, ln = m[1], n
		} else if m := genericFileLineRe2.FindStringSubmatch(line); m != nil {
			// Only accept this form when the path looks like a path.
			if strings.Contains(m[1], "/") || strings.Contains(m[1], "\\") || strings.Contains(m[1], ".") {
				n, _ := strconv.Atoi(m[2])
				file, ln = m[1], n
			}
		}
		sev := "error"
		lower := strings.ToLower(line)
		if strings.Contains(lower, "failed") || strings.Contains(lower, "failure") {
			sev = "error"
		}
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
			File: file, Line: ln, Severity: sev,
			Message: truncateMsg(strings.TrimSpace(line), 500),
			Context: ctx,
		})
	}
	return res, nil
}
