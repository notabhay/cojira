// Package confluence provides Confluence identifier resolution utilities.
package confluence

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	cerrors "github.com/cojira/cojira/internal/errors"
)

// PageGetter is the minimal interface needed to resolve page identifiers
// that require an API lookup (display URLs, space:title format).
type PageGetter interface {
	GetPageByTitle(space, title string) (map[string]any, error)
}

var (
	pagesIDRe  = regexp.MustCompile(`/pages/(\d+)`)
	tinyRe     = regexp.MustCompile(`/x/([A-Za-z0-9+/_-]+)`)
	bareTinyRe = regexp.MustCompile(`^[A-Za-z0-9+/_-]{3,}$`)
	pageIDQSRe = regexp.MustCompile(`[?&]pageId=(\d+)`)
)

// TinyCodeToPageID decodes a Confluence tiny link code to a numeric page ID.
func TinyCodeToPageID(code string) (int64, error) {
	code = strings.TrimSpace(code)
	code = strings.Trim(code, "/")

	b64 := padBase64(code)
	raw, err := base64.URLEncoding.DecodeString(b64)
	if err != nil {
		return 0, fmt.Errorf("unable to decode tiny code %q: %w", code, err)
	}
	if len(raw) == 0 {
		return 0, fmt.Errorf("tiny code %q decoded to empty bytes", code)
	}

	// Little-endian unsigned integer.
	var result uint64
	for i, b := range raw {
		result |= uint64(b) << (8 * i)
	}
	_ = binary.LittleEndian // reference for clarity
	return int64(result), nil
}

func padBase64(s string) string {
	pad := (4 - len(s)%4) % 4
	return s + strings.Repeat("=", pad)
}

// ResolvePageID resolves a flexible page identifier to a numeric page ID.
//
// Supported formats:
//   - "default" or "root" (returns defaultPageID if set)
//   - Numeric ID: "12345"
//   - URL with pageId param: "...?pageId=12345"
//   - URL with /pages/ID/: ".../pages/12345/..."
//   - Tiny link URL: ".../x/CODE"
//   - Display URL: ".../display/SPACE/Title"
//   - URL with spaceKey + title query params
//   - Bare tiny code: "APnAVAE"
//   - Space:Title: SPACE:"My Page Title" or SPACE:MyPage
func ResolvePageID(client PageGetter, identifier string, defaultPageID string) (string, error) {
	identifier = strings.TrimSpace(identifier)

	lower := strings.ToLower(identifier)
	if defaultPageID != "" && (lower == "default" || lower == "root") {
		return defaultPageID, nil
	}

	// Pure numeric ID.
	if isDigits(identifier) {
		return identifier, nil
	}

	// URL identifiers.
	if strings.HasPrefix(identifier, "http://") || strings.HasPrefix(identifier, "https://") {
		return resolveFromURL(client, identifier)
	}

	// URL-ish strings without scheme.
	if strings.Contains(identifier, "pageId=") {
		if m := pageIDQSRe.FindStringSubmatch(identifier); m != nil {
			return m[1], nil
		}
	}
	if strings.Contains(identifier, "/pages/") {
		if m := pagesIDRe.FindStringSubmatch(identifier); m != nil {
			return m[1], nil
		}
	}
	if strings.Contains(identifier, "/x/") {
		if m := tinyRe.FindStringSubmatch(identifier); m != nil {
			id, err := TinyCodeToPageID(m[1])
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%d", id), nil
		}
	}

	// Bare tiny code (alphanumeric, 3+ chars, no colon).
	if bareTinyRe.MatchString(identifier) && !strings.Contains(identifier, ":") {
		id, err := TinyCodeToPageID(identifier)
		if err == nil {
			return fmt.Sprintf("%d", id), nil
		}
		// Fall through to space:title.
	}

	// Space:Title format.
	if strings.Contains(identifier, ":") {
		parts := strings.SplitN(identifier, ":", 2)
		if len(parts) == 2 {
			spaceKey := strings.TrimSpace(parts[0])
			title := strings.TrimSpace(parts[1])
			title = strings.Trim(title, `"'`)
			if spaceKey != "" && title != "" {
				return resolveByTitle(client, spaceKey, title)
			}
		}
	}

	return "", fmt.Errorf("could not resolve page identifier: %q", identifier)
}

func resolveFromURL(client PageGetter, identifier string) (string, error) {
	parsed, err := url.Parse(identifier)
	if err != nil {
		return "", fmt.Errorf("unrecognized Confluence URL: %q", identifier)
	}

	qs := parsed.Query()

	// URL with pageId query param.
	if vals := qs["pageId"]; len(vals) > 0 && vals[0] != "" {
		if isDigits(vals[0]) {
			return vals[0], nil
		}
	}

	path := parsed.Path

	// URL with /pages/ID/ path.
	if m := pagesIDRe.FindStringSubmatch(path); m != nil {
		return m[1], nil
	}

	// Tiny link: /x/CODE.
	if m := tinyRe.FindStringSubmatch(path); m != nil {
		id, err := TinyCodeToPageID(m[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", id), nil
	}

	// Display URL: /display/SPACE/Page+Title.
	if strings.Contains(path, "/display/") {
		segments := splitPathSegments(path)
		idx := indexOf(segments, "display")
		if idx != -1 && idx+2 < len(segments) {
			spaceKey := segments[idx+1]
			titlePart := strings.Join(segments[idx+2:], "/")
			title, err := url.PathUnescape(titlePart)
			if err != nil {
				title = titlePart
			}
			// Also handle '+' as spaces (url.PathUnescape doesn't handle +).
			title = strings.ReplaceAll(title, "+", " ")
			return resolveByTitle(client, spaceKey, title)
		}
	}

	// viewpage.action with spaceKey + title.
	if spaceVals := qs["spaceKey"]; len(spaceVals) > 0 && spaceVals[0] != "" {
		if titleVals := qs["title"]; len(titleVals) > 0 && titleVals[0] != "" {
			return resolveByTitle(client, spaceVals[0], titleVals[0])
		}
	}

	return "", fmt.Errorf("unrecognized Confluence URL: %q", identifier)
}

func resolveByTitle(client PageGetter, space, title string) (string, error) {
	page, err := client.GetPageByTitle(space, title)
	if err != nil {
		return "", &cerrors.CojiraError{
			Code:     cerrors.IdentUnresolved,
			Message:  fmt.Sprintf("Network error resolving page: %v", err),
			ExitCode: 1,
		}
	}
	if page == nil {
		return "", fmt.Errorf("page not found: space=%q, title=%q", space, title)
	}
	id, ok := page["id"]
	if !ok {
		return "", fmt.Errorf("page not found: space=%q, title=%q", space, title)
	}
	return fmt.Sprintf("%v", id), nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func splitPathSegments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func indexOf(ss []string, target string) int {
	for i, s := range ss {
		if s == target {
			return i
		}
	}
	return -1
}
