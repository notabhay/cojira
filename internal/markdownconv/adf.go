package markdownconv

import (
	"encoding/json"
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

const FormatJiraADF = "jira-adf"

// ToJiraADFJSON converts Markdown into an Atlassian Document Format JSON string.
func ToJiraADFJSON(input string) (string, error) {
	doc, err := ToJiraADF(input)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ToJiraADF converts Markdown into an Atlassian Document Format document.
func ToJiraADF(input string) (map[string]any, error) {
	normalized := normalizeMarkdown(input)
	if strings.TrimSpace(normalized) == "" {
		return emptyADFDocument(), nil
	}

	source := []byte(normalized)
	doc := jiraMarkdown.Parser().Parse(text.NewReader(source))
	renderer := &adfRenderer{source: source}
	content := renderer.renderBlocks(doc)
	if len(content) == 0 {
		return emptyADFDocument(), nil
	}
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": content,
	}, nil
}

// PlainTextToJiraADF converts plain text into a simple ADF document.
func PlainTextToJiraADF(input string) map[string]any {
	normalized := normalizeMarkdown(input)
	trimmed := strings.TrimSpace(normalized)
	if trimmed == "" {
		return emptyADFDocument()
	}

	paragraphs := splitPlainParagraphs(normalized)
	content := make([]map[string]any, 0, len(paragraphs))
	for _, para := range paragraphs {
		nodes := plainTextInlineContent(para)
		if len(nodes) == 0 {
			continue
		}
		content = append(content, map[string]any{
			"type":    "paragraph",
			"content": nodes,
		})
	}
	if len(content) == 0 {
		return emptyADFDocument()
	}
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": content,
	}
}

func emptyADFDocument() map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []map[string]any{},
	}
}

type adfRenderer struct {
	source []byte
}

func (r *adfRenderer) renderBlocks(node ast.Node) []map[string]any {
	blocks := []map[string]any{}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		blocks = append(blocks, r.renderBlock(child)...)
	}
	return blocks
}

func (r *adfRenderer) renderBlock(node ast.Node) []map[string]any {
	switch n := node.(type) {
	case *ast.Heading:
		inline := r.renderInlineChildren(n, nil)
		if len(inline) == 0 {
			return nil
		}
		return []map[string]any{{
			"type":    "heading",
			"attrs":   map[string]any{"level": n.Level},
			"content": inline,
		}}
	case *ast.Paragraph, *ast.TextBlock:
		inline := r.renderInlineChildren(node, nil)
		if len(inline) == 0 {
			return nil
		}
		return []map[string]any{{
			"type":    "paragraph",
			"content": inline,
		}}
	case *ast.Blockquote:
		content := r.renderBlocks(n)
		if len(content) == 0 {
			return nil
		}
		return []map[string]any{{
			"type":    "blockquote",
			"content": content,
		}}
	case *ast.FencedCodeBlock:
		return []map[string]any{r.renderCodeBlock(string(n.Language(r.source)), string(n.Text(r.source)))}
	case *ast.CodeBlock:
		return []map[string]any{r.renderCodeBlock("", string(n.Text(r.source)))}
	case *ast.List:
		return []map[string]any{r.renderList(n)}
	case *ast.ThematicBreak:
		return []map[string]any{{"type": "rule"}}
	case *extast.Table:
		table := r.renderTable(n)
		if table == nil {
			return nil
		}
		return []map[string]any{table}
	default:
		return r.renderBlocks(node)
	}
}

func (r *adfRenderer) renderCodeBlock(language, raw string) map[string]any {
	node := map[string]any{
		"type": "codeBlock",
		"content": []map[string]any{{
			"type": "text",
			"text": strings.TrimRight(raw, "\n"),
		}},
	}
	if strings.TrimSpace(language) != "" {
		node["attrs"] = map[string]any{"language": strings.TrimSpace(language)}
	}
	return node
}

func (r *adfRenderer) renderList(list *ast.List) map[string]any {
	listType := "bulletList"
	if list.IsOrdered() {
		listType = "orderedList"
	}
	items := []map[string]any{}
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		content := r.renderBlocks(item)
		if len(content) == 0 {
			continue
		}
		items = append(items, map[string]any{
			"type":    "listItem",
			"content": content,
		})
	}
	return map[string]any{
		"type":    listType,
		"content": items,
	}
}

func (r *adfRenderer) renderTable(table *extast.Table) map[string]any {
	rows := []map[string]any{}
	for child := table.FirstChild(); child != nil; child = child.NextSibling() {
		switch child.Kind() {
		case extast.KindTableHeader:
			rows = append(rows, r.renderTableRow(child, true))
		case extast.KindTableRow:
			rows = append(rows, r.renderTableRow(child, false))
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return map[string]any{
		"type":    "table",
		"content": rows,
	}
}

func (r *adfRenderer) renderTableRow(row ast.Node, header bool) map[string]any {
	cells := []map[string]any{}
	cellType := "tableCell"
	if header {
		cellType = "tableHeader"
	}
	for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
		inline := r.renderInlineChildren(cell, nil)
		if len(inline) == 0 {
			inline = []map[string]any{{"type": "text", "text": ""}}
		}
		cells = append(cells, map[string]any{
			"type": cellType,
			"content": []map[string]any{{
				"type":    "paragraph",
				"content": inline,
			}},
		})
	}
	return map[string]any{
		"type":    "tableRow",
		"content": cells,
	}
}

func (r *adfRenderer) renderInlineChildren(node ast.Node, marks []map[string]any) []map[string]any {
	out := []map[string]any{}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		out = append(out, r.renderInlineNode(child, marks)...)
	}
	return out
}

func (r *adfRenderer) renderInlineNode(node ast.Node, marks []map[string]any) []map[string]any {
	switch n := node.(type) {
	case *ast.Text:
		textValue := string(n.Text(r.source))
		if textValue == "" {
			return nil
		}
		out := []map[string]any{textNode(textValue, marks)}
		if n.HardLineBreak() || n.SoftLineBreak() {
			out = append(out, map[string]any{"type": "hardBreak"})
		}
		return out
	case *ast.String:
		if len(n.Value) == 0 {
			return nil
		}
		return []map[string]any{textNode(string(n.Value), marks)}
	case *ast.Emphasis:
		nextMarks := appendMark(marks, emphasisMark(n.Level))
		return r.renderInlineChildren(n, nextMarks)
	case *extast.Strikethrough:
		return r.renderInlineChildren(n, appendMark(marks, map[string]any{"type": "strike"}))
	case *ast.CodeSpan:
		textValue := strings.TrimSpace(inlinePlainText(r.source, n))
		if textValue == "" {
			return nil
		}
		return []map[string]any{textNode(textValue, appendMark(marks, map[string]any{"type": "code"}))}
	case *ast.Link:
		href := strings.TrimSpace(string(n.Destination))
		nextMarks := marks
		if href != "" {
			nextMarks = appendMark(nextMarks, map[string]any{"type": "link", "attrs": map[string]any{"href": href}})
		}
		return r.renderInlineChildren(n, nextMarks)
	case *ast.AutoLink:
		href := strings.TrimSpace(string(n.URL(r.source)))
		if href == "" {
			return nil
		}
		label := strings.TrimSpace(string(n.Label(r.source)))
		if label == "" {
			label = href
		}
		return []map[string]any{textNode(label, appendMark(marks, map[string]any{"type": "link", "attrs": map[string]any{"href": href}}))}
	case *extast.TaskCheckBox:
		if n.IsChecked {
			return []map[string]any{textNode("[x] ", marks)}
		}
		return []map[string]any{textNode("[ ] ", marks)}
	case *ast.Image:
		alt := strings.TrimSpace(inlinePlainText(r.source, n))
		if alt == "" {
			alt = strings.TrimSpace(string(n.Destination))
		}
		if alt == "" {
			return nil
		}
		return []map[string]any{textNode(alt, marks)}
	default:
		return r.renderInlineChildren(node, marks)
	}
}

func emphasisMark(level int) map[string]any {
	if level >= 2 {
		return map[string]any{"type": "strong"}
	}
	return map[string]any{"type": "em"}
}

func appendMark(existing []map[string]any, mark map[string]any) []map[string]any {
	next := make([]map[string]any, 0, len(existing)+1)
	next = append(next, existing...)
	next = append(next, mark)
	return next
}

func textNode(value string, marks []map[string]any) map[string]any {
	node := map[string]any{
		"type": "text",
		"text": value,
	}
	if len(marks) > 0 {
		cloned := make([]map[string]any, 0, len(marks))
		for _, mark := range marks {
			cloned = append(cloned, mark)
		}
		node["marks"] = cloned
	}
	return node
}

func inlinePlainText(source []byte, node ast.Node) string {
	var builder strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Text:
			builder.Write(n.Text(source))
			if n.HardLineBreak() || n.SoftLineBreak() {
				builder.WriteString("\n")
			}
		case *ast.String:
			builder.Write(n.Value)
		default:
			builder.WriteString(inlinePlainText(source, child))
		}
	}
	return builder.String()
}

func splitPlainParagraphs(input string) []string {
	raw := strings.Split(strings.TrimSpace(input), "\n\n")
	out := make([]string, 0, len(raw))
	for _, paragraph := range raw {
		trimmed := strings.TrimSpace(paragraph)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func plainTextInlineContent(paragraph string) []map[string]any {
	parts := strings.Split(paragraph, "\n")
	content := make([]map[string]any, 0, len(parts)*2)
	for i, part := range parts {
		if part != "" {
			content = append(content, map[string]any{"type": "text", "text": part})
		}
		if i < len(parts)-1 {
			content = append(content, map[string]any{"type": "hardBreak"})
		}
	}
	return content
}
