package confluence

import (
	"encoding/xml"
	"fmt"
	"html"
	"strings"
)

type markdownListState struct {
	ordered bool
	index   int
}

type markdownRenderer struct {
	builder      strings.Builder
	lists        []markdownListState
	links        []string
	inPre        int
	inCode       int
	warnings     []string
	tableCell    int
	pendingSpace bool
}

func storageToMarkdown(input string) (string, []string, error) {
	wrapped := `<root xmlns:ac="ac" xmlns:ri="ri" xmlns:at="at">` + input + `</root>`
	decoder := xml.NewDecoder(strings.NewReader(wrapped))
	renderer := &markdownRenderer{}
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return "", nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if err := renderer.start(decoder, typed); err != nil {
				return "", nil, err
			}
		case xml.EndElement:
			renderer.end(typed)
		case xml.CharData:
			renderer.text(string(typed))
		}
	}
	return finalizeMarkdown(renderer.builder.String()), uniqueStrings(renderer.warnings), nil
}

func (r *markdownRenderer) start(decoder *xml.Decoder, start xml.StartElement) error {
	name := strings.ToLower(start.Name.Local)
	switch name {
	case "p", "div", "section":
		r.ensureParagraph()
	case "br":
		r.builder.WriteString("\n")
	case "h1", "h2", "h3", "h4", "h5", "h6":
		r.ensureParagraph()
		level := 1
		fmt.Sscanf(name, "h%d", &level)
		r.builder.WriteString(strings.Repeat("#", level) + " ")
	case "ul":
		r.lists = append(r.lists, markdownListState{})
	case "ol":
		r.lists = append(r.lists, markdownListState{ordered: true, index: 1})
	case "li":
		r.ensureLine()
		indent := strings.Repeat("  ", max(0, len(r.lists)-1))
		prefix := "- "
		if len(r.lists) > 0 && r.lists[len(r.lists)-1].ordered {
			prefix = fmt.Sprintf("%d. ", r.lists[len(r.lists)-1].index)
			r.lists[len(r.lists)-1].index++
		}
		r.builder.WriteString(indent + prefix)
	case "strong", "b":
		r.flushPendingSpace()
		r.builder.WriteString("**")
	case "em", "i":
		r.flushPendingSpace()
		r.builder.WriteString("*")
	case "code":
		if r.inPre == 0 {
			r.flushPendingSpace()
			r.inCode++
			r.builder.WriteString("`")
		}
	case "pre":
		r.ensureParagraph()
		r.inPre++
		r.builder.WriteString("```\n")
	case "a":
		r.flushPendingSpace()
		r.builder.WriteString("[")
		r.links = append(r.links, attrValue(start, "href"))
	case "table":
		r.ensureParagraph()
	case "tr":
		r.ensureLine()
		r.tableCell = 0
	case "th", "td":
		if r.tableCell == 0 {
			r.builder.WriteString("| ")
		} else {
			r.builder.WriteString(" | ")
		}
		r.tableCell++
	case "img":
		src := attrValue(start, "src")
		alt := attrValue(start, "alt")
		if alt == "" {
			alt = "image"
		}
		r.builder.WriteString(fmt.Sprintf("![%s](%s)", alt, src))
	case "structured-macro", "macro":
		nameAttr := attrValue(start, "name")
		if nameAttr == "" {
			nameAttr = attrValue(start, "ac:name")
		}
		if nameAttr == "" {
			nameAttr = "unknown"
		}
		r.ensureParagraph()
		r.builder.WriteString(fmt.Sprintf("[Confluence macro: %s]\n\n", nameAttr))
		r.warnings = append(r.warnings, fmt.Sprintf("Preserved macro placeholder for %q during markdown export.", nameAttr))
		return skipElement(decoder, start.Name)
	case "page", "attachment", "user":
		title := attrValue(start, "ri:content-title")
		if title == "" {
			title = attrValue(start, "ri:filename")
		}
		if title == "" {
			title = attrValue(start, "ri:username")
		}
		if title != "" {
			r.builder.WriteString(title)
		}
	}
	return nil
}

func (r *markdownRenderer) end(end xml.EndElement) {
	name := strings.ToLower(end.Name.Local)
	switch name {
	case "p", "div", "section", "h1", "h2", "h3", "h4", "h5", "h6":
		r.ensureParagraph()
	case "ul", "ol":
		if len(r.lists) > 0 {
			r.lists = r.lists[:len(r.lists)-1]
		}
		r.ensureParagraph()
	case "li":
		r.ensureLine()
	case "strong", "b":
		r.builder.WriteString("**")
	case "em", "i":
		r.builder.WriteString("*")
	case "code":
		if r.inPre == 0 && r.inCode > 0 {
			r.inCode--
			r.builder.WriteString("`")
		}
	case "pre":
		if r.inPre > 0 {
			r.inPre--
		}
		r.builder.WriteString("\n```\n\n")
	case "a":
		href := ""
		if len(r.links) > 0 {
			href = r.links[len(r.links)-1]
			r.links = r.links[:len(r.links)-1]
		}
		if href != "" {
			r.builder.WriteString("](" + href + ")")
		} else {
			r.builder.WriteString("]")
		}
	case "tr":
		if r.tableCell > 0 {
			r.builder.WriteString(" |\n")
			r.tableCell = 0
		}
	}
}

func (r *markdownRenderer) text(value string) {
	if r.inPre > 0 {
		r.builder.WriteString(html.UnescapeString(value))
		return
	}
	text := strings.Join(strings.Fields(html.UnescapeString(value)), " ")
	hasTrailingSpace := strings.TrimRightFunc(value, isSpaceRune) != value
	if text == "" {
		r.pendingSpace = r.pendingSpace || hasTrailingSpace
		return
	}
	if r.pendingSpace && needsSpace(r.builder.String(), text) {
		r.builder.WriteString(" ")
	}
	r.pendingSpace = false
	if needsSpace(r.builder.String(), text) {
		r.builder.WriteString(" ")
	}
	r.builder.WriteString(text)
	r.pendingSpace = hasTrailingSpace
}

func (r *markdownRenderer) ensureParagraph() {
	current := r.builder.String()
	if current == "" {
		r.pendingSpace = false
		return
	}
	if strings.HasSuffix(current, "\n\n") {
		r.pendingSpace = false
		return
	}
	if strings.HasSuffix(current, "\n") {
		r.builder.WriteString("\n")
		r.pendingSpace = false
		return
	}
	r.builder.WriteString("\n\n")
	r.pendingSpace = false
}

func (r *markdownRenderer) ensureLine() {
	current := r.builder.String()
	if current == "" || strings.HasSuffix(current, "\n") {
		r.pendingSpace = false
		return
	}
	r.builder.WriteString("\n")
	r.pendingSpace = false
}

func (r *markdownRenderer) flushPendingSpace() {
	if !r.pendingSpace {
		return
	}
	if current := r.builder.String(); current != "" && !strings.HasSuffix(current, "\n") && !strings.HasSuffix(current, " ") {
		r.builder.WriteString(" ")
	}
	r.pendingSpace = false
}

func skipElement(decoder *xml.Decoder, name xml.Name) error {
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local == name.Local {
				depth++
			}
		case xml.EndElement:
			if typed.Name.Local == name.Local {
				depth--
			}
		}
	}
	return nil
}

func attrValue(start xml.StartElement, name string) string {
	for _, attr := range start.Attr {
		full := attr.Name.Local
		if attr.Name.Space != "" {
			full = attr.Name.Space + ":" + attr.Name.Local
		}
		if strings.EqualFold(full, name) || strings.EqualFold(attr.Name.Local, name) {
			return attr.Value
		}
	}
	return ""
}

func finalizeMarkdown(input string) string {
	lines := strings.Split(input, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, trimmed)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func needsSpace(current, next string) bool {
	if current == "" {
		return false
	}
	last := current[len(current)-1]
	first := next[0]
	if strings.ContainsRune(".,;:!?)", rune(first)) {
		return false
	}
	return last != '\n' && last != ' ' && last != '[' && last != '(' && last != '/' && last != '>' && last != '*' && last != '`'
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isSpaceRune(r rune) bool {
	switch r {
	case ' ', '\n', '\t', '\r':
		return true
	default:
		return false
	}
}
