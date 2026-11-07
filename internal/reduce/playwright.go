package reduce

import (
	"encoding/json"
	"strings"
)

// Playwright JSON reporter parser. Suites nest, so walk recursively.

type pwReport struct {
	Suites []pwSuite `json:"suites"`
	Stats  *pwStats  `json:"stats"`
}

type pwStats struct {
	Total      int `json:"total"`
	Expected   int `json:"expected"`
	Unexpected int `json:"unexpected"`
	Skipped    int `json:"skipped"`
}

type pwSuite struct {
	Title  string    `json:"title"`
	Suites []pwSuite `json:"suites"`
	Specs  []pwSpec  `json:"specs"`
}

type pwSpec struct {
	Title string   `json:"title"`
	Tests []pwTest `json:"tests"`
}

type pwTest struct {
	Title   string     `json:"title"`
	Results []pwResult `json:"results"`
}

type pwResult struct {
	Status   string      `json:"status"`
	Location *pwLocation `json:"location"`
	Error    *pwError    `json:"error"`
}

type pwLocation struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

type pwError struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

func parsePlaywright(data []byte) (*parseResult, error) {
	var rep pwReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, InvalidInput("playwright", err.Error())
	}
	if rep.Suites == nil && rep.Stats == nil {
		return nil, InvalidInput("playwright", "document is not a Playwright JSON report")
	}
	res := &parseResult{}
	total, passed, failed, skipped := 0, 0, 0, 0
	var walkSuite func(s pwSuite)
	var walkSpec func(spec pwSpec, suiteTitle string)
	walkSpec = func(spec pwSpec, suiteTitle string) {
		for _, t := range spec.Tests {
			for _, r := range t.Results {
				total++
				switch r.Status {
				case "passed":
					passed++
				case "failed", "timedOut", "interrupted":
					failed++
				case "skipped":
					skipped++
				default:
					// "expected" style statuses are not failures.
					if r.Status == "expected" {
						passed++
					}
				}
				if r.Status != "failed" && r.Status != "timedOut" && r.Status != "interrupted" {
					continue
				}
				msg := ""
				var ctxText string
				if r.Error != nil {
					msg = firstLine(r.Error.Message)
					ctxText = r.Error.Message
					if r.Error.Stack != "" {
						ctxText += "\n" + r.Error.Stack
					}
				}
				if msg == "" {
					msg = "Test failed: " + t.Title
				}
				file, line := "", 0
				if r.Location != nil {
					file, line = r.Location.File, r.Location.Line
				}
				testName := joinTitles(suiteTitle, spec.Title, t.Title)
				res.findings = append(res.findings, Finding{
					File: file, Line: line, Severity: "error",
					Message: truncateMsg(msg, 500), Test: testName,
					Context: contextFromText(ctxText),
				})
			}
		}
	}
	walkSuite = func(s pwSuite) {
		for _, sub := range s.Suites {
			walkSuite(sub)
		}
		for _, spec := range s.Specs {
			walkSpec(spec, s.Title)
		}
	}
	for _, s := range rep.Suites {
		walkSuite(s)
	}
	if rep.Stats != nil && rep.Stats.Total > 0 {
		res.summary = Summary{Total: rep.Stats.Total, Passed: rep.Stats.Expected, Failed: rep.Stats.Unexpected, Skipped: rep.Stats.Skipped}
	} else {
		res.summary = Summary{Total: total, Passed: passed, Failed: failed, Skipped: skipped}
	}
	return res, nil
}

// joinTitles joins title parts, dropping consecutive duplicates
// (Playwright fixtures often repeat the same title at every level).
func joinTitles(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == p {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}
