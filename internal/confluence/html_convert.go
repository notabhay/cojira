package confluence

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
)

var (
	htmlBreakRe      = regexp.MustCompile(`(?is)<br\s*/?>`)
	htmlParagraphRe  = regexp.MustCompile(`(?is)</p\s*>`)
	htmlDivCloseRe   = regexp.MustCompile(`(?is)</div\s*>`)
	htmlListItemOpen = regexp.MustCompile(`(?is)<li\b[^>]*>`)
	htmlListItemEnd  = regexp.MustCompile(`(?is)</li\s*>`)
	htmlTableRowEnd  = regexp.MustCompile(`(?is)</tr\s*>`)
	htmlHeadingOpen  = regexp.MustCompile(`(?is)<h([1-6])\b[^>]*>`)
	htmlHeadingEnd   = regexp.MustCompile(`(?is)</h([1-6])\s*>`)
	htmlStrongOpen   = regexp.MustCompile(`(?is)<(strong|b)\b[^>]*>`)
	htmlStrongEnd    = regexp.MustCompile(`(?is)</(strong|b)\s*>`)
	htmlEmOpen       = regexp.MustCompile(`(?is)<(em|i)\b[^>]*>`)
	htmlEmEnd        = regexp.MustCompile(`(?is)</(em|i)\s*>`)
	htmlCodeOpen     = regexp.MustCompile(`(?is)<code\b[^>]*>`)
	htmlCodeEnd      = regexp.MustCompile(`(?is)</code\s*>`)
	blankLinesRe     = regexp.MustCompile(`\n{3,}`)
)

func renderViewContent(content string, format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "html", "raw":
		return content, nil
	case "text":
		return htmlToText(content), nil
	case "markdown":
		return htmlToMarkdown(content), nil
	default:
		return "", &cerrors.CojiraError{
			Code:     cerrors.Unsupported,
			Message:  fmt.Sprintf("Unsupported Confluence view format %q. Use html, text, or markdown.", format),
			ExitCode: 2,
		}
	}
}

func htmlToText(content string) string {
	text := htmlBreakRe.ReplaceAllString(content, "\n")
	text = htmlParagraphRe.ReplaceAllString(text, "\n\n")
	text = htmlDivCloseRe.ReplaceAllString(text, "\n")
	text = htmlListItemOpen.ReplaceAllString(text, "- ")
	text = htmlListItemEnd.ReplaceAllString(text, "\n")
	text = htmlTableRowEnd.ReplaceAllString(text, "\n")
	text = htmlHeadingOpen.ReplaceAllString(text, "")
	text = htmlHeadingEnd.ReplaceAllString(text, "\n\n")
	text = stripHTML(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = blankLinesRe.ReplaceAllString(text, "\n\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func htmlToMarkdown(content string) string {
	text := content
	text = htmlHeadingOpen.ReplaceAllStringFunc(text, func(match string) string {
		sub := htmlHeadingOpen.FindStringSubmatch(match)
		if len(sub) != 2 {
			return ""
		}
		level := strings.Repeat("#", int(sub[1][0]-'0'))
		return level + " "
	})
	text = htmlHeadingEnd.ReplaceAllString(text, "\n\n")
	text = htmlBreakRe.ReplaceAllString(text, "\n")
	text = htmlParagraphRe.ReplaceAllString(text, "\n\n")
	text = htmlDivCloseRe.ReplaceAllString(text, "\n")
	text = htmlListItemOpen.ReplaceAllString(text, "- ")
	text = htmlListItemEnd.ReplaceAllString(text, "\n")
	text = htmlStrongOpen.ReplaceAllString(text, "**")
	text = htmlStrongEnd.ReplaceAllString(text, "**")
	text = htmlEmOpen.ReplaceAllString(text, "*")
	text = htmlEmEnd.ReplaceAllString(text, "*")
	text = htmlCodeOpen.ReplaceAllString(text, "`")
	text = htmlCodeEnd.ReplaceAllString(text, "`")
	text = htmlTagRe.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = blankLinesRe.ReplaceAllString(text, "\n\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
