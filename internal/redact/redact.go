// Package redact removes secrets and simple PII before durable message storage.
package redact

import (
	"regexp"
	"strings"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{12,}`),
}

var keyValueSecretPattern = regexp.MustCompile(`(?i)((password|token|secret|authorization|api[_-]?key)"?\s*[:=]\s*)"?[^"\s,}]+\"?`)
var phonePattern = regexp.MustCompile(`(?m)(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)`)

// Content redacts sensitive values while keeping enough context for audit/debug.
func Content(s string) string {
	out := keyValueSecretPattern.ReplaceAllString(s, `${1}"***"`)
	for _, re := range secretPatterns {
		out = re.ReplaceAllStringFunc(out, func(match string) string {
			fields := strings.Fields(match)
			if len(fields) > 0 && strings.EqualFold(fields[0], "bearer") {
				return fields[0] + " ***"
			}
			return "***"
		})
	}
	out = phonePattern.ReplaceAllString(out, `${1}1**********${2}`)
	return out
}
