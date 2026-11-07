package reduce

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Vitest JSON reporter parser.

type vitestReport struct {
	NumTotalTests   int                `json:"numTotalTests"`
	NumPassedTests  int                `json:"numPassedTests"`
	NumFailedTests  int                `json:"numFailedTests"`
	NumPendingTests int                `json:"numPendingTests"`
	TestResults     []vitestFileResult `json:"testResults"`
}

type vitestFileResult struct {
	Name             string            `json:"name"`
	AssertionResults []vitestAssertion `json:"assertionResults"`
}

type vitestAssertion struct {
	Title           string          `json:"title"`
	FullName        string          `json:"fullName"`
	Status          string          `json:"status"`
	FailureMessages []string        `json:"failureMessages"`
	Location        *vitestLocation `json:"location"`
}

type vitestLocation struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

var atFileLineRe = regexp.MustCompile(`([^\s():'"]+\.(?:ts|tsx|js|jsx|mts|cts)):(\d+):(\d+)`)

func parseVitest(data []byte) (*parseResult, error) {
	var rep vitestReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return nil, InvalidInput("vitest", err.Error())
	}
	if rep.TestResults == nil && rep.NumTotalTests == 0 {
		return nil, InvalidInput("vitest", "document is not a Vitest JSON report")
	}
	res := &parseResult{}
	res.summary = Summary{Total: rep.NumTotalTests, Passed: rep.NumPassedTests, Failed: rep.NumFailedTests, Skipped: rep.NumPendingTests}
	derivedTotal, derivedPassed, derivedFailed, derivedSkipped := 0, 0, 0, 0
	for _, fr := range rep.TestResults {
		for _, a := range fr.AssertionResults {
			derivedTotal++
			switch a.Status {
			case "passed":
				derivedPassed++
			case "failed":
				derivedFailed++
			case "pending", "skipped":
				derivedSkipped++
			}
			if a.Status != "failed" {
				continue
			}
			joined := strings.Join(a.FailureMessages, "\n")
			msg := firstLine(joined)
			if msg == "" {
				msg = a.Title
			}
			file, line := fr.Name, 0
			if a.Location != nil {
				line = a.Location.Line
			}
			if line == 0 {
				if f, ln := fileLineFromText(joined); f != "" {
					file, line = f, ln
				}
			}
			res.findings = append(res.findings, Finding{
				File: file, Line: line, Severity: "error",
				Message: truncateMsg(msg, 500), Test: firstNonEmpty(a.FullName, a.Title),
				Context: contextFromText(joined),
			})
		}
	}
	if rep.NumTotalTests == 0 {
		res.summary = Summary{Total: derivedTotal, Passed: derivedPassed, Failed: derivedFailed, Skipped: derivedSkipped}
	}
	return res, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func fileLineFromText(s string) (string, int) {
	for _, l := range strings.Split(s, "\n") {
		if m := atFileLineRe.FindStringSubmatch(l); m != nil {
			n, _ := strconv.Atoi(m[2])
			return m[1], n
		}
	}
	return "", 0
}
