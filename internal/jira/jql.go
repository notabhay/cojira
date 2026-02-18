package jira

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	projectEqualsRe = regexp.MustCompile(`(?i)\bproject\s*=`)
	projectInRe     = regexp.MustCompile(`(?i)\bproject\s+in\b`)
	orderByRe       = regexp.MustCompile(`(?i)\border\s+by\b`)
)

// JQLValue returns a JQL-safe value (quoted when needed, preserving functions).
func JQLValue(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return `""`
	}
	// Already quoted.
	if (strings.HasPrefix(raw, `"`) || strings.HasPrefix(raw, `'`)) &&
		(strings.HasSuffix(raw, `"`) || strings.HasSuffix(raw, `'`)) {
		return raw
	}
	// JQL function call.
	if strings.Contains(raw, "(") && strings.HasSuffix(raw, ")") {
		return raw
	}
	// Negative numeric (unquoted operator).
	if strings.HasPrefix(raw, "-") {
		return raw
	}
	return fmt.Sprintf(`"%s"`, raw)
}

// StripJQLStrings replaces all quoted string content in JQL with spaces,
// preserving character positions. This allows safe regex matching on JQL
// structure without matching inside string literals.
func StripJQLStrings(jql string) string {
	var out []byte
	var quote byte
	escape := false

	for i := 0; i < len(jql); i++ {
		ch := jql[i]
		if quote != 0 {
			if escape {
				escape = false
				out = append(out, ' ')
				continue
			}
			if ch == '\\' {
				escape = true
				out = append(out, ' ')
				continue
			}
			if ch == quote {
				quote = 0
				out = append(out, ' ')
			} else {
				out = append(out, ' ')
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			out = append(out, ' ')
			continue
		}
		out = append(out, ch)
	}
	return string(out)
}

// StripJQLOrderBy removes a trailing ORDER BY clause (outside quoted strings).
func StripJQLOrderBy(jql string) string {
	raw := strings.TrimSpace(jql)
	if raw == "" {
		return ""
	}
	sanitized := StripJQLStrings(raw)
	loc := orderByRe.FindStringIndex(sanitized)
	if loc == nil {
		return raw
	}
	return strings.TrimSpace(raw[:loc[0]])
}

// JQLHasProject returns true if the JQL contains a project = or project in clause
// outside of quoted strings.
func JQLHasProject(jql string) bool {
	sanitized := StripJQLStrings(jql)
	if projectEqualsRe.MatchString(sanitized) {
		return true
	}
	return projectInRe.MatchString(sanitized)
}

// FixJQLShellEscapes fixes common shell-mangled JQL operators
// (e.g. bash history expansion turning ! into \!).
func FixJQLShellEscapes(jql string) string {
	if strings.Contains(jql, `\!`) {
		jql = strings.ReplaceAll(jql, `\!`, "!")
	}
	return jql
}

// ApplyDefaultJQLScope prepends a default JQL scope (e.g. "project = PROJ")
// if the JQL does not already contain a project clause.
func ApplyDefaultJQLScope(jql string, scope string) string {
	jql = FixJQLShellEscapes(jql)
	if scope == "" {
		return jql
	}
	if JQLHasProject(jql) {
		return jql
	}
	return fmt.Sprintf("(%s) AND (%s)", scope, jql)
}
