package reduce

import (
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
)

// TRX (.NET) parser. Uses the XML reporter, never text regex for the
// test results themselves; a regex is only used to pull file:line out of
// a stack trace frame.

// Matches " in C:\path\file.cs:line 12" and "file.cs:line 12".
var trxFrameRe = regexp.MustCompile(`([A-Za-z0-9_\\/\.\-]+\.(?:cs|vb|fs)):line (\d+)`)
var trxFrameRe2 = regexp.MustCompile(` in ([^\s:]+):line (\d+)`)
var trxDriveRe = regexp.MustCompile(`([A-Za-z]:\\[^\s]+?\.(?:cs|vb|fs)):line (\d+)`)

type trxRun struct {
	XMLName       xml.Name   `xml:"TestRun"`
	Results       trxResults `xml:"Results"`
	ResultSummary trxSummary `xml:"ResultSummary"`
}

type trxResults struct {
	UnitTestResults []trxUnitTestResult `xml:"UnitTestResult"`
}

type trxUnitTestResult struct {
	TestName string    `xml:"testName,attr"`
	Outcome  string    `xml:"outcome,attr"`
	Duration string    `xml:"duration,attr"`
	Output   trxOutput `xml:"Output"`
}

type trxOutput struct {
	ErrorInfo trxErrorInfo `xml:"ErrorInfo"`
}

type trxErrorInfo struct {
	Message    string `xml:"Message"`
	StackTrace string `xml:"StackTrace"`
}

type trxSummary struct {
	Counters trxCounters `xml:"Counters"`
}

type trxCounters struct {
	Total       int `xml:"total,attr"`
	Executed    int `xml:"executed,attr"`
	Passed      int `xml:"passed,attr"`
	Failed      int `xml:"failed,attr"`
	Error       int `xml:"error,attr"`
	NotExecuted int `xml:"notExecuted,attr"`
}

func parseTRX(data []byte) (*parseResult, error) {
	var run trxRun
	if err := xml.Unmarshal(data, &run); err != nil {
		return nil, InvalidInput("trx", err.Error())
	}
	// A file that parses as XML but is not a TRX run is invalid input.
	if len(run.Results.UnitTestResults) == 0 && run.ResultSummary.Counters.Total == 0 {
		// Distinguish an empty-but-valid TRX (no tests) from a wrong file:
		// a real TRX root has a Results element; xml.Unmarshal gives no
		// signal for that, so require the TestRun name to have been seen.
		// If the document had no TestRun root, Unmarshal would still
		// succeed with empty fields, so check for the literal marker.
		if !strings.Contains(string(data), "TestRun") {
			return nil, InvalidInput("trx", "document is not a TRX TestRun")
		}
	}
	res := &parseResult{}
	c := run.ResultSummary.Counters
	res.summary = Summary{Total: c.Total, Passed: c.Passed, Failed: c.Failed, Errors: c.Error, Skipped: c.NotExecuted}
	seenFailed := 0
	for _, t := range run.Results.UnitTestResults {
		if !strings.EqualFold(t.Outcome, "Failed") && !strings.EqualFold(t.Outcome, "Error") {
			continue
		}
		seenFailed++
		msg := firstLine(t.Output.ErrorInfo.Message)
		if msg == "" {
			msg = "Test failed: " + t.TestName
		}
		file, line := trxFileLine(t.Output.ErrorInfo.StackTrace)
		ctx, cut := contextFromText(t.Output.ErrorInfo.Message, t.Output.ErrorInfo.StackTrace)
		if cut {
			res.truncated = true
		}
		res.findings = append(res.findings, Finding{
			File: file, Line: line, Severity: "error",
			Message: truncateMsg(msg, 500), Test: t.TestName, Context: ctx,
		})
	}
	// If the counters were absent, derive the summary from the findings.
	if c.Total == 0 && len(run.Results.UnitTestResults) > 0 {
		passed := 0
		for _, t := range run.Results.UnitTestResults {
			if strings.EqualFold(t.Outcome, "Passed") {
				passed++
			}
		}
		res.summary = Summary{Total: len(run.Results.UnitTestResults), Passed: passed, Failed: seenFailed, Errors: 0, Skipped: 0}
	}
	return res, nil
}

func trxFileLine(stack string) (string, int) {
	for _, l := range strings.Split(stack, "\n") {
		if m := trxDriveRe.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			return strings.ReplaceAll(m[1], "\\", "/"), n
		}
		if m := trxFrameRe2.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			return strings.ReplaceAll(m[1], "\\", "/"), n
		}
		if m := trxFrameRe.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			return strings.ReplaceAll(m[1], "\\", "/"), n
		}
	}
	return "", 0
}

// contextFromText builds up to MaxContextLines from message + stack trace.
// It reports whether lines were cut, so Reduce can set Truncated: the
// cap lives here for memory reasons, but the flag lives in one place.
func contextFromText(parts ...string) ([]string, bool) {
	var out []string
	for _, p := range parts {
		for _, l := range strings.Split(stripANSI(p), "\n") {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			out = append(out, l)
			if len(out) >= MaxContextLines {
				return out, true
			}
		}
	}
	return out, false
}
