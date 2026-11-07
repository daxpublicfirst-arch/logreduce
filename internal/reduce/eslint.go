package reduce

import (
	"encoding/json"
	"strings"
)

// ESLint JSON formatter parser.

type eslintFile struct {
	FilePath string          `json:"filePath"`
	Messages []eslintMessage `json:"messages"`
}

type eslintMessage struct {
	Line     int    `json:"line"`
	Severity int    `json:"severity"` // 2 = error, 1 = warning
	Message  string `json:"message"`
	RuleID   string `json:"ruleId"`
}

func parseESLint(data []byte) (*parseResult, error) {
	var files []eslintFile
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, InvalidInput("eslint", err.Error())
	}
	res := &parseResult{}
	for _, f := range files {
		for _, m := range f.Messages {
			sev := "warning"
			if m.Severity >= 2 {
				sev = "error"
			}
			msg := m.Message
			if m.RuleID != "" {
				msg = msg + " (" + m.RuleID + ")"
			}
			res.findings = append(res.findings, Finding{
				File: f.FilePath, Line: m.Line, Severity: sev,
				Message: truncateMsg(strings.TrimSpace(msg), 500),
			})
		}
	}
	return res, nil
}
