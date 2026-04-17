package output

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"
)

// TableString renders a simple tab-aligned table for human output.
func TableString(headers []string, rows [][]string) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	if len(headers) > 0 {
		_, _ = fmt.Fprintln(w, strings.Join(headers, "\t"))
		dividers := make([]string, len(headers))
		for i, header := range headers {
			if header == "" {
				dividers[i] = ""
				continue
			}
			dividers[i] = strings.Repeat("-", len(header))
		}
		_, _ = fmt.Fprintln(w, strings.Join(dividers, "\t"))
	}
	for _, row := range rows {
		_, _ = fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
	return strings.TrimRight(buf.String(), "\n")
}

// Truncate shortens a string for compact table display.
func Truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "…"
}

// StatusBadge returns a compact, optionally colorized status token.
func StatusBadge(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "-"
	}
	label := status
	if !ShouldColorize() {
		return label
	}
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "done"), strings.Contains(lower, "closed"), strings.Contains(lower, "resolved"), strings.Contains(lower, "complete"):
		return "\x1b[32m" + label + ansiReset
	case strings.Contains(lower, "blocked"), strings.Contains(lower, "fail"), strings.Contains(lower, "error"):
		return "\x1b[31m" + label + ansiReset
	case strings.Contains(lower, "progress"), strings.Contains(lower, "review"), strings.Contains(lower, "doing"), strings.Contains(lower, "active"):
		return "\x1b[33m" + label + ansiReset
	case strings.Contains(lower, "todo"), strings.Contains(lower, "to do"), strings.Contains(lower, "open"), strings.Contains(lower, "new"):
		return "\x1b[36m" + label + ansiReset
	default:
		return label
	}
}
