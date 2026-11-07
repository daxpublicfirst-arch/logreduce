// Package reduce turns one build, test or lint log into structured findings.
//
// It is harness-independent, makes no model calls and opens no socket.
// It reads only the --log path and writes only under --out.
package reduce

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Version is the tool version, matching plugin.toml and the v0.1.0 tag.
const Version = "0.1.0"

// ToolName is the tool name used in output and telemetry.
const ToolName = "logreduce"

// MaxInputBytes is the input limit from plugin.toml (200 MB).
const MaxInputBytes = 200 * 1024 * 1024

// MaxFindings caps the findings list; beyond it Truncated is set.
const MaxFindings = 1000

// MaxContextLines caps context lines per finding.
const MaxContextLines = 10

// MaxUnparsedFindings caps raw findings kept for unparseable failure lines.
const MaxUnparsedFindings = 50

// Kinds lists the supported --kind values, in the order shown in usage.
var Kinds = []string{"trx", "vitest", "playwright", "tsc", "eslint", "pytest", "go-test", "generic"}

// ValidKind reports whether k is a supported kind.
func ValidKind(k string) bool {
	for _, v := range Kinds {
		if v == k {
			return true
		}
	}
	return false
}

// Finding is one failure, with file:line and context.
type Finding struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Test     string   `json:"test,omitempty"`
	Context  []string `json:"context,omitempty"`
}

// Summary counts tests / findings, depending on the kind. For lint-like
// kinds (tsc, eslint, generic) there are no test counts, so Total is the
// finding count, Failed/Errors are the error-severity finding count, and
// Passed/Skipped are 0. See README for the per-kind mapping.
type Summary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Errors  int `json:"errors"`
	Skipped int `json:"skipped"`
}

// Response is the stdout JSON object for `reduce`, also written to
// <out>/findings.json.
type Response struct {
	OK         bool      `json:"ok"`
	Tool       string    `json:"tool"`
	Version    string    `json:"version"`
	Kind       string    `json:"kind"`
	Summary    Summary   `json:"summary"`
	Findings   []Finding `json:"findings"`
	Unparsed   int       `json:"unparsed"`
	FullLog    string    `json:"full_log"`
	Truncated  bool      `json:"truncated"`
	DurationMs int64     `json:"duration_ms"`
}

// ErrorDetail is the inner object of a failure response.
type ErrorDetail struct {
	Code        string `json:"code"`
	Blocked     string `json:"blocked"`
	Fix         string `json:"fix"`
	IfThatFails string `json:"if_that_fails"`
}

// ErrorResponse is the one JSON object printed on failure.
type ErrorResponse struct {
	OK    bool        `json:"ok"`
	Error ErrorDetail `json:"error"`
}

// ToolError is a failure with a machine-readable code and user-facing help.
type ToolError struct {
	Code        string
	Blocked     string
	Fix         string
	IfThatFails string
	// Exit is 2 for usage/input errors, 3 for tool failures.
	Exit int
}

func (e *ToolError) Error() string { return e.Blocked }

func usageError(code, blocked, fix, ifThatFails string) *ToolError {
	return &ToolError{Code: code, Blocked: blocked, Fix: fix, IfThatFails: ifThatFails, Exit: 2}
}

func toolError(code, blocked, fix, ifThatFails string) *ToolError {
	return &ToolError{Code: code, Blocked: blocked, Fix: fix, IfThatFails: ifThatFails, Exit: 3}
}

// InputMissing is returned when --log does not exist.
func InputMissing(path string) *ToolError {
	return usageError("INPUT_MISSING",
		"Log file not found: "+path,
		"Pass an existing file with --log <path>.",
		"Check the path spelling and that the file was written before running logreduce.")
}

// UnknownKind is returned for an unsupported --kind.
func UnknownKind(kind string) *ToolError {
	return usageError("UNKNOWN_KIND",
		"Unknown kind: "+kind,
		"Use one of: "+strings.Join(Kinds, ", ")+".",
		"Run `logreduce --help` to list the supported kinds.")
}

// InvalidInput is returned when the log cannot be parsed as its kind.
func InvalidInput(kind, detail string) *ToolError {
	return usageError("INVALID_INPUT",
		"Could not parse log as "+kind+": "+detail,
		"Regenerate the log with the reporter's JSON/XML output for kind "+kind+" and retry.",
		"Try --kind generic, which accepts plain text logs.")
}

// parseResult is what a kind parser returns.
type parseResult struct {
	summary   Summary
	findings  []Finding
	unparsed  int
	truncated bool
}

// Reduce reads the log at logPath, parses it as kind, writes full.log and
// findings.json under outDir, and returns the response. It writes nowhere
// else. durationMs is measured by the caller and filled in before the
// response is marshalled, so stdout and findings.json are identical bytes.
func Reduce(kind, logPath, outDir string, start time.Time) (*Response, []byte, error) {
	if !ValidKind(kind) {
		return nil, nil, UnknownKind(kind)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, InputMissing(logPath)
		}
		return nil, nil, usageError("INPUT_UNREADABLE", "Cannot read log file: "+logPath+": "+err.Error(), "Check the file permissions and retry.", "Copy the log to a readable location and retry.")
	}
	if info.IsDir() {
		return nil, nil, usageError("INPUT_UNREADABLE", "Log path is a directory, not a file: "+logPath, "Pass a file with --log <path>.", "Point --log at the log file itself, not its directory.")
	}

	// Read the input, honouring the 200 MB limit. Anything beyond the
	// limit is dropped and Truncated is set (see ANALYSIS / README).
	truncated := false
	var data []byte
	f, err := os.Open(logPath)
	if err != nil {
		return nil, nil, usageError("INPUT_UNREADABLE", "Cannot open log file: "+logPath+": "+err.Error(), "Check the file permissions and retry.", "Copy the log to a readable location and retry.")
	}
	defer f.Close()
	if info.Size() > MaxInputBytes {
		truncated = true
		data = make([]byte, MaxInputBytes)
		if _, err := io.ReadFull(f, data); err != nil {
			return nil, nil, toolError("TOOL_FAILURE", "Failed reading log file: "+err.Error(), "Retry with a smaller log file.", "Split the log and run logreduce on each part.")
		}
	} else {
		data, err = io.ReadAll(f)
		if err != nil {
			return nil, nil, toolError("TOOL_FAILURE", "Failed reading log file: "+err.Error(), "Retry with a smaller log file.", "Split the log and run logreduce on each part.")
		}
	}
	inputBytes := len(data)

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, nil, toolError("TOOL_FAILURE", "Cannot create output directory: "+outDir+": "+err.Error(), "Pass a writable directory with --out <dir>.", "Choose a different --out directory.")
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return nil, nil, toolError("TOOL_FAILURE", "Cannot resolve output directory: "+err.Error(), "Pass a plain relative or absolute path with --out.", "Run from a different working directory and retry.")
	}
	fullLogPath := filepath.Join(absOut, "full.log")
	if err := os.WriteFile(fullLogPath, data, 0o644); err != nil {
		return nil, nil, toolError("TOOL_FAILURE", "Cannot write full.log: "+err.Error(), "Pass a writable directory with --out <dir>.", "Choose a different --out directory.")
	}

	res, err := parse(kind, data)
	if err != nil {
		return nil, nil, err
	}
	if res.truncated {
		truncated = true
	}

	findings := res.findings
	if findings == nil {
		findings = []Finding{}
	}
	// Cap context, sort deterministically, cap findings.
	for i := range findings {
		if len(findings[i].Context) > MaxContextLines {
			findings[i].Context = findings[i].Context[:MaxContextLines]
			truncated = true
		}
		findings[i].Context = cleanContext(findings[i].Context)
		if findings[i].Severity == "" {
			findings[i].Severity = "error"
		}
	}
	sortFindings(findings)
	if len(findings) > MaxFindings {
		findings = findings[:MaxFindings]
		truncated = true
	}

	// Lint-like kinds: derive the summary from findings when the parser
	// did not supply test counts.
	summary := res.summary
	if kind == "tsc" || kind == "eslint" || kind == "generic" {
		errCount := 0
		for _, fd := range findings {
			if fd.Severity == "error" {
				errCount++
			}
		}
		summary = Summary{Total: len(findings), Passed: 0, Failed: errCount, Errors: errCount, Skipped: 0}
	}

	resp := &Response{
		OK:         true,
		Tool:       ToolName,
		Version:    Version,
		Kind:       kind,
		Summary:    summary,
		Findings:   findings,
		Unparsed:   res.unparsed,
		FullLog:    fullLogPath,
		Truncated:  truncated,
		DurationMs: time.Since(start).Milliseconds(),
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, nil, toolError("TOOL_FAILURE", "Failed encoding output JSON: "+err.Error(), "Retry the run.", "Report this as a bug with the log file attached.")
	}
	out = append(out, '\n')
	if err := os.WriteFile(filepath.Join(absOut, "findings.json"), out, 0o644); err != nil {
		return nil, nil, toolError("TOOL_FAILURE", "Cannot write findings.json: "+err.Error(), "Pass a writable directory with --out <dir>.", "Choose a different --out directory.")
	}
	_ = inputBytes
	return resp, out, nil
}

func parse(kind string, data []byte) (*parseResult, error) {
	switch kind {
	case "trx":
		return parseTRX(data)
	case "vitest":
		return parseVitest(data)
	case "playwright":
		return parsePlaywright(data)
	case "tsc":
		return parseTSC(data)
	case "eslint":
		return parseESLint(data)
	case "pytest":
		return parsePytest(data)
	case "go-test":
		return parseGoTest(data)
	case "generic":
		return parseGeneric(data)
	}
	return nil, UnknownKind(kind)
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		if fs[i].Line != fs[j].Line {
			return fs[i].Line < fs[j].Line
		}
		if fs[i].Severity != fs[j].Severity {
			return fs[i].Severity < fs[j].Severity
		}
		return fs[i].Message < fs[j].Message
	})
}

func cleanContext(ctx []string) []string {
	out := ctx[:0]
	for _, l := range ctx {
		l = strings.TrimRight(l, " \t\r")
		if l == "" {
			continue
		}
		out = append(out, l)
	}
	if out == nil {
		return nil
	}
	return out
}

// firstLine returns the first non-empty line of s, ANSI-stripped.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = stripANSI(strings.TrimSpace(l))
		if l != "" {
			return l
		}
	}
	return ""
}

// stripANSI removes ANSI escape sequences from s. Fast path: most log
// lines contain no ESC byte at all, so return them unchanged.
func stripANSI(s string) string {
	if strings.IndexByte(s, 0x1b) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !((s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z')) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// splitLines splits on \n, stripping \r, keeping order.
func splitLines(data []byte) []string {
	return strings.Split(string(data), "\n")
}

// truncateMsg caps a message to a sane length.
func truncateMsg(s string, max int) string {
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
