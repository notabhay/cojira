package jira

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	browseKeyRe = regexp.MustCompile(`/browse/([A-Za-z][A-Za-z0-9_]+-\d+)`)
	issueKeyRe  = regexp.MustCompile(`/issue/([A-Za-z][A-Za-z0-9_]+-\d+|\d+)`)
)

// ResolveIssueIdentifier resolves a flexible issue identifier to a Jira issue key or ID.
func ResolveIssueIdentifier(identifier string) string {
	ident := strings.TrimSpace(identifier)

	// URL formats.
	if strings.HasPrefix(ident, "http://") || strings.HasPrefix(ident, "https://") {
		parsed, err := url.Parse(ident)
		if err == nil {
			// /browse/KEY-123
			if m := browseKeyRe.FindStringSubmatch(parsed.Path); m != nil {
				return m[1]
			}
			// /rest/api/.../issue/KEY-123
			if m := issueKeyRe.FindStringSubmatch(parsed.Path); m != nil {
				return m[1]
			}
			// Query params.
			qs := parsed.Query()
			for _, key := range []string{"issueId", "issueIdOrKey", "selectedIssue"} {
				if vals := qs[key]; len(vals) > 0 && vals[0] != "" {
					return vals[0]
				}
			}
		}
	}

	// Path-ish formats.
	if m := browseKeyRe.FindStringSubmatch(ident); m != nil {
		return m[1]
	}
	if m := issueKeyRe.FindStringSubmatch(ident); m != nil {
		return m[1]
	}

	return ident
}

// InferBaseURL infers the Jira base URL from a full issue URL.
// Returns empty string if the identifier is not a URL.
func InferBaseURL(identifier string) string {
	ident := strings.TrimSpace(identifier)
	if !strings.HasPrefix(ident, "http://") && !strings.HasPrefix(ident, "https://") {
		return ""
	}

	parsed, err := url.Parse(ident)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	path := parsed.Path
	basePath := ""

	switch {
	case strings.Contains(path, "/browse/"):
		basePath = strings.SplitN(path, "/browse/", 2)[0]
	case strings.Contains(path, "/rest/api/"):
		basePath = strings.SplitN(path, "/rest/api/", 2)[0]
	case strings.Contains(path, "/secure/"):
		basePath = strings.SplitN(path, "/secure/", 2)[0]
	case strings.Contains(path, "/software/"):
		// Jira Software next-gen URLs.
		// Guard for the rare case where /software IS the context path:
		// the path will start with /software/software/ (doubled segment).
		if strings.HasPrefix(path, "/software/software/") {
			basePath = "/software"
		} else {
			basePath = strings.SplitN(path, "/software/", 2)[0]
		}
	case strings.Contains(path, "/projects/"):
		basePath = strings.SplitN(path, "/projects/", 2)[0]
	case strings.Contains(path, "/plugins/"):
		basePath = strings.SplitN(path, "/plugins/", 2)[0]
	default:
		basePath = strings.TrimRight(path, "/")
	}

	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host+basePath, "/")
}
