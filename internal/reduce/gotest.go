package reduce

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// go test -json parser: one JSON object per line.

type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

var goFileLineRe = regexp.MustCompile(`([^\s:]+\.go):(\d+):`)

func parseGoTest(data []byte) (*parseResult, error) {
	res := &parseResult{}
	lines := splitLines(data)
	sawAny := false
	// outputs per test, in order, for context.
	outputs := map[string][]string{}
	order := []string{}
	seenTests := map[string]bool{}
	passed, failed, skipped := 0, 0, 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var ev goTestEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// A non-JSON line in a go test -json stream: it looked like
			// output, not a failure — ignore it, do not count unparsed.
			continue
		}
		sawAny = true
		if ev.Test != "" {
			if !seenTests[ev.Test] {
				seenTests[ev.Test] = true
				order = append(order, ev.Test)
			}
		}
		switch ev.Action {
		case "output":
			if ev.Test != "" {
				outputs[ev.Test] = append(outputs[ev.Test], stripANSI(strings.TrimRight(ev.Output, "\n")))
			}
		case "pass":
			if ev.Test != "" {
				passed++
			}
		case "fail":
			if ev.Test != "" {
				failed++
				outs := outputs[ev.Test]
				file, ln := goFileLineFromOutputs(outs)
				msg := goFailMessage(ev.Test, outs)
				ctx, cut := lastNonEmpty(outs, MaxContextLines)
				if cut {
					res.truncated = true
				}
				res.findings = append(res.findings, Finding{
					File: file, Line: ln, Severity: "error",
					Message: truncateMsg(msg, 500), Test: ev.Test, Context: ctx,
				})
			}
		case "skip":
			if ev.Test != "" {
				skipped++
			}
		}
	}
	if !sawAny {
		return nil, InvalidInput("go-test", "input is not go test -json output (no JSON lines)")
	}
	total := passed + failed + skipped
	res.summary = Summary{Total: total, Passed: passed, Failed: failed, Skipped: skipped}
	_ = order
	return res, nil
}

func goFileLineFromOutputs(outs []string) (string, int) {
	for _, l := range outs {
		if m := goFileLineRe.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			return m[1], n
		}
	}
	return "", 0
}

func goFailMessage(test string, outs []string) string {
	for _, l := range outs {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "--- FAIL") {
			continue
		}
		if strings.Contains(t, "Error:") || strings.Contains(t, "expected") || strings.Contains(t, "FAIL:") {
			return t
		}
	}
	// Fall back to the last non-empty output line.
	for i := len(outs) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(outs[i]); t != "" {
			return t
		}
	}
	return "Test failed: " + test
}

func lastNonEmpty(outs []string, n int) ([]string, bool) {
	var cleaned []string
	for _, l := range outs {
		l = strings.TrimRight(l, " \t\r")
		if l == "" {
			continue
		}
		cleaned = append(cleaned, l)
	}
	if len(cleaned) > n {
		return cleaned[len(cleaned)-n:], true
	}
	return cleaned, false
}
