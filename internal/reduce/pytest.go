package reduce

import (
	"encoding/json"
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
)

// pytest parser: pytest-json-report JSON first, JUnit XML as fallback.

type pytestReport struct {
	Tests   []pytestTest   `json:"tests"`
	Summary *pytestSummary `json:"summary"`
}

type pytestSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Error   int `json:"error"`
}

type pytestTest struct {
	NodeID  string      `json:"nodeid"`
	Outcome string      `json:"outcome"`
	Call    *pytestCall `json:"call"`
}

type pytestCall struct {
	Longrepr json.RawMessage `json:"longrepr"`
}

var pyFileLineRe = regexp.MustCompile(`([^\s:]+\.py):(\d+):?`)

func parsePytest(data []byte) (*parseResult, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		return parsePytestJSON(data)
	}
	if strings.HasPrefix(trimmed, "<") {
		return parsePytestJUnit(data)
	}
	return nil, InvalidInput("pytest", "input is neither pytest JSON report nor JUnit XML")
}

func parsePytestJSON(data []byte) (*parseResult, error) {
	var rep pytestReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, InvalidInput("pytest", err.Error())
	}
	if rep.Tests == nil && rep.Summary == nil {
		return nil, InvalidInput("pytest", "document is not a pytest JSON report")
	}
	res := &parseResult{}
	passed, failed, skipped, errors := 0, 0, 0, 0
	for _, t := range rep.Tests {
		switch t.Outcome {
		case "passed":
			passed++
		case "failed":
			failed++
		case "skipped":
			skipped++
		case "error":
			errors++
			failed++
		}
		if t.Outcome != "failed" && t.Outcome != "error" {
			continue
		}
		longrepr := ""
		if t.Call != nil && len(t.Call.Longrepr) > 0 {
			longrepr = longreprString(t.Call.Longrepr)
		}
		file, line := pytestNodeFileLine(t.NodeID, longrepr)
		msg := pytestMessage(longrepr)
		if msg == "" {
			msg = "Test failed: " + t.NodeID
		}
		res.findings = append(res.findings, Finding{
			File: file, Line: line, Severity: "error",
			Message: truncateMsg(msg, 500), Test: t.NodeID,
			Context: contextFromText(longrepr),
		})
	}
	if rep.Summary != nil {
		res.summary = Summary{Total: rep.Summary.Total, Passed: rep.Summary.Passed, Failed: rep.Summary.Failed, Errors: rep.Summary.Error, Skipped: rep.Summary.Skipped}
	} else {
		res.summary = Summary{Total: len(rep.Tests), Passed: passed, Failed: failed, Errors: errors, Skipped: skipped}
	}
	return res, nil
}

// longrepr may be a string or a list of strings / objects.
func longreprString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		var parts []string
		for _, item := range arr {
			var str string
			if err := json.Unmarshal(item, &str); err == nil {
				parts = append(parts, str)
				continue
			}
			var obj struct {
				// pytest longrepr entries can be objects with a message.
				Message string `json:"message"`
			}
			if err := json.Unmarshal(item, &obj); err == nil && obj.Message != "" {
				parts = append(parts, obj.Message)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func pytestNodeFileLine(nodeid, longrepr string) (string, int) {
	// nodeid: tests/test_x.py::TestClass::test_name  (or with ::line?)
	file := nodeid
	if i := strings.Index(nodeid, "::"); i >= 0 {
		file = nodeid[:i]
	}
	if f, ln := pyFileLineFromText(longrepr); f != "" {
		// Prefer the traceback location when it points at the test file.
		if f == file || strings.HasSuffix(f, file) {
			return f, ln
		}
		return f, ln
	}
	return file, 0
}

func pyFileLineFromText(s string) (string, int) {
	// Last file:line in a traceback is usually the failing assert site;
	// take the last match, not the first.
	var file string
	line := 0
	for _, l := range strings.Split(s, "\n") {
		if m := pyFileLineRe.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			file, line = m[1], n
		}
	}
	return file, line
}

// JUnit XML fallback (pytest --junitxml).
type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}
type junitSuite struct {
	Name      string      `xml:"name,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Errors    int         `xml:"errors,attr"`
	Skipped   int         `xml:"skipped,attr"`
	TestCases []junitCase `xml:"testcase"`
}
type junitCase struct {
	Classname string        `xml:"classname,attr"`
	Name      string        `xml:"name,attr"`
	File      string        `xml:"file,attr"`
	Line      int           `xml:"line,attr"`
	Failure   *junitFailure `xml:"failure"`
	Error     *junitFailure `xml:"error"`
	Skipped   *struct{}     `xml:"skipped"`
}
type junitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func parsePytestJUnit(data []byte) (*parseResult, error) {
	var suites junitSuites
	if err := xml.Unmarshal(data, &suites); err != nil {
		return nil, InvalidInput("pytest", err.Error())
	}
	// A single <testsuite> root also parses into Suites via a wrapper:
	if len(suites.Suites) == 0 {
		var single junitSuite
		if err := xml.Unmarshal(data, &single); err == nil && len(single.TestCases) > 0 {
			suites.Suites = []junitSuite{single}
		}
	}
	if len(suites.Suites) == 0 {
		return nil, InvalidInput("pytest", "document is not JUnit XML")
	}
	res := &parseResult{}
	total, passed, failed, errors, skipped := 0, 0, 0, 0, 0
	for _, s := range suites.Suites {
		total += s.Tests
		failed += s.Failures
		errors += s.Errors
		skipped += s.Skipped
		for _, tc := range s.TestCases {
			if tc.Failure == nil && tc.Error == nil {
				if tc.Skipped != nil {
					// already counted in suite skipped
				} else {
					passed++
				}
				continue
			}
			f := tc.Failure
			if f == nil {
				f = tc.Error
			}
			file, line := tc.File, tc.Line
			if file == "" {
				file = strings.ReplaceAll(tc.Classname, ".", "/") + ".py"
			}
			if line == 0 {
				if fl, ln := pyFileLineFromText(f.Text); fl != "" {
					file, line = fl, ln
				}
			}
			msg := firstNonEmpty(f.Message, firstLine(f.Text))
			if msg == "" {
				msg = "Test failed: " + tc.Name
			}
			res.findings = append(res.findings, Finding{
				File: file, Line: line, Severity: "error",
				Message: truncateMsg(msg, 500), Test: tc.Classname + "::" + tc.Name,
				Context: contextFromText(f.Text),
			})
		}
	}
	if total == 0 {
		total = passed + failed + errors + skipped
	}
	res.summary = Summary{Total: total, Passed: passed, Failed: failed, Errors: errors, Skipped: skipped}
	return res, nil
}

// pytestMessage picks the meaningful error line from a longrepr: the
// last "E   ..." assertion line or "*Error: ..." line, falling back
// to the first non-empty line.
func pytestMessage(longrepr string) string {
	lines := strings.Split(longrepr, "\n")
	best := ""
	for _, l := range lines {
		t := strings.TrimSpace(stripANSI(l))
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "E   ") || strings.HasPrefix(t, "E ") {
			best = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, "E   "), "E "))
		} else if strings.Contains(t, "Error:") || strings.Contains(t, "Error ") {
			best = t
		}
	}
	if best != "" {
		return best
	}
	return firstLine(longrepr)
}
