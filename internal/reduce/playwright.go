package reduce

import (
	"encoding/json"
	"strings"
)

// Playwright JSON reporter parser. Suites nest, so walk recursively.
//
// Format notes (verified against the real reporter types in
// playwright@1.51 types/testReporter.d.ts and real report files — see
// docs/REAL_WORLD.md):
//   - Top level is {config, suites, errors, stats}; stats is
//     {expected, unexpected, flaky, skipped} — there is NO total.
//   - Suites nest (suites[].suites) and hold specs[].
//   - A spec carries file/line (the test definition); a test carries
//     status "expected" | "unexpected" | "flaky" | "skipped" — never
//     "failed".
//   - A result carries status "passed" | "failed" | "timedOut" |
//     "skipped" | "interrupted". There is NO results[].location.
//     The failing file:line lives on the error (error.location,
//     errors[0].location, errorLocation) and falls back to the spec.
//   - error.message is optional; errors[] and error.stack are fallbacks.
//   - A test is retried as multiple results; findings are ONE per
//     failing test (its last failed result), not one per result.
//     A "flaky" test passed on its last retry, so it is not a finding.

type pwReport struct {
	Suites []pwSuite `json:"suites"`
	Stats  *pwStats  `json:"stats"`
}

type pwStats struct {
	Expected   int `json:"expected"`
	Unexpected int `json:"unexpected"`
	Flaky      int `json:"flaky"`
	Skipped    int `json:"skipped"`
	// "total" does not exist in the real format; accepted only as a
	// tolerance for hand-written reports.
	Total int `json:"total"`
}

type pwSuite struct {
	Title  string    `json:"title"`
	Suites []pwSuite `json:"suites"`
	Specs  []pwSpec  `json:"specs"`
}

type pwSpec struct {
	Title  string   `json:"title"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
	Column int      `json:"column"`
	Tests  []pwTest `json:"tests"`
}

type pwTest struct {
	Title   string     `json:"title"`
	Status  string     `json:"status"`
	Results []pwResult `json:"results"`
}

type pwResult struct {
	Status        string      `json:"status"`
	Location      *pwLocation `json:"location"` // legacy; real reports do not set this
	Error         *pwError    `json:"error"`
	Errors        []pwError   `json:"errors"`
	ErrorLocation *pwLocation `json:"errorLocation"`
}

type pwLocation struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type pwError struct {
	Message  string      `json:"message"`
	Stack    string      `json:"stack"`
	Location *pwLocation `json:"location"`
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
			total++
			// Find the last failed result, if any.
			var lastFailed *pwResult
			anyFailed := false
			for i := range t.Results {
				switch t.Results[i].Status {
				case "failed", "timedOut", "interrupted":
					anyFailed = true
					lastFailed = &t.Results[i]
				}
			}
			failing := t.Status == "unexpected" || (t.Status == "" && anyFailed)
			switch t.Status {
			case "expected":
				passed++
			case "unexpected":
				failed++
			case "flaky":
				// Passed on the last retry; not a finding.
				passed++
			case "skipped":
				skipped++
			default:
				// No test status (old/hand-written report): derive it.
				if anyFailed {
					failed++
				} else {
					passed++
				}
			}
			if !failing || lastFailed == nil {
				continue
			}
			r := lastFailed
			msg, ctxText := pwFailureMessage(r, t.Title)
			file, line := pwFailureFileLine(r, spec)
			testName := joinTitles(suiteTitle, spec.Title, t.Title)
			ctx, cut := contextFromText(ctxText)
			if cut {
				res.truncated = true
			}
			res.findings = append(res.findings, Finding{
				File: file, Line: line, Severity: "error",
				Message: truncateMsg(msg, 500), Test: testName,
				Context: ctx,
			})
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
	if rep.Stats != nil {
		statTotal := rep.Stats.Expected + rep.Stats.Unexpected + rep.Stats.Flaky + rep.Stats.Skipped
		if statTotal > 0 {
			res.summary = Summary{Total: statTotal, Passed: rep.Stats.Expected + rep.Stats.Flaky, Failed: rep.Stats.Unexpected, Skipped: rep.Stats.Skipped}
		} else if rep.Stats.Total > 0 {
			res.summary = Summary{Total: rep.Stats.Total, Passed: rep.Stats.Expected, Failed: rep.Stats.Unexpected, Skipped: rep.Stats.Skipped}
		} else {
			res.summary = Summary{Total: total, Passed: passed, Failed: failed, Skipped: skipped}
		}
	} else {
		res.summary = Summary{Total: total, Passed: passed, Failed: failed, Skipped: skipped}
	}
	return res, nil
}

// pwFailureMessage picks the error text: error.message, then
// errors[0].message, then the first line of error.stack.
func pwFailureMessage(r *pwResult, title string) (string, string) {
	msg := ""
	var ctxText string
	err := r.Error
	if err == nil && len(r.Errors) > 0 {
		err = &r.Errors[0]
	}
	if err != nil {
		msg = firstLine(err.Message)
		ctxText = err.Message
		if err.Stack != "" {
			ctxText += "\n" + err.Stack
		}
		if msg == "" && err.Stack != "" {
			msg = firstLine(err.Stack)
		}
	}
	if msg == "" && len(r.Errors) > 0 {
		for _, e := range r.Errors {
			if m := firstLine(e.Message); m != "" {
				msg = m
				ctxText += "\n" + e.Message
				break
			}
		}
	}
	if msg == "" {
		msg = "Test failed: " + title
	}
	return msg, ctxText
}

// pwFailureFileLine returns the failing file:line: error.location,
// then errors[0].location, then errorLocation, then the legacy
// results[].location, then the spec's file:line.
func pwFailureFileLine(r *pwResult, spec pwSpec) (string, int) {
	if r.Error != nil && r.Error.Location != nil {
		return r.Error.Location.File, r.Error.Location.Line
	}
	if len(r.Errors) > 0 && r.Errors[0].Location != nil {
		return r.Errors[0].Location.File, r.Errors[0].Location.Line
	}
	if r.ErrorLocation != nil {
		return r.ErrorLocation.File, r.ErrorLocation.Line
	}
	if r.Location != nil {
		return r.Location.File, r.Location.Line
	}
	return spec.File, spec.Line
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
