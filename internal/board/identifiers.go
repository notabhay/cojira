// Package board provides board identifier resolution and configuration types
// for Jira board swimlanes and detail view fields.
package board

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	boardIDRe    = regexp.MustCompile(`^\d+$`)
	boardsPathRe = regexp.MustCompile(`/boards/(\d+)`)
	rapidViewRe  = regexp.MustCompile(`rapidView(?:Id)?=(\d+)`)
)

// ResolveBoardIdentifier resolves a flexible board identifier (numeric ID, URL,
// or query string) to a plain board ID string.
func ResolveBoardIdentifier(identifier string) string {
	raw := strings.TrimSpace(identifier)
	if raw == "" {
		return raw
	}

	if boardIDRe.MatchString(raw) {
		return raw
	}

	// Bare query-string-ish inputs like rapidView=45434.
	if strings.Contains(raw, "rapidView=") || strings.Contains(raw, "rapidViewId=") {
		qs, err := url.ParseQuery(strings.TrimLeft(raw, "?"))
		if err == nil {
			for _, key := range []string{"rapidView", "rapidViewId"} {
				if vals := qs[key]; len(vals) > 0 && vals[0] != "" {
					return vals[0]
				}
			}
		}
	}

	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := url.Parse(raw)
		if err == nil {
			qs := parsed.Query()
			for _, key := range []string{"rapidView", "rapidViewId"} {
				if vals := qs[key]; len(vals) > 0 && vals[0] != "" {
					return vals[0]
				}
			}
			if m := boardsPathRe.FindStringSubmatch(parsed.Path); m != nil {
				return m[1]
			}
		}
	}

	// Fallback regex for rapidView= anywhere in the string.
	if m := rapidViewRe.FindStringSubmatch(raw); m != nil {
		return m[1]
	}

	return raw
}
