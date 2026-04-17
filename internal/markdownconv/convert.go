package markdownconv

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	renderhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

const (
	FormatMarkdown          = "markdown"
	FormatConfluenceStorage = "confluence-storage"
	FormatJiraWiki          = "jira-wiki"
)

// Convert converts markdown input into one of the supported output formats.
func Convert(input, to string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(to)) {
	case FormatConfluenceStorage:
		return ToConfluenceStorage(input)
	case FormatJiraWiki:
		return ToJiraWiki(input)
	case FormatJiraADF:
		return ToJiraADFJSON(input)
	default:
		return "", fmt.Errorf("unsupported conversion target: %s", to)
	}
}

var confluenceMarkdown = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.Strikethrough,
		extension.Linkify,
	),
	goldmark.WithRendererOptions(
		renderhtml.WithXHTML(),
		renderhtml.WithHardWraps(),
	),
)

var jiraMarkdown = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.Strikethrough,
		extension.Linkify,
	),
)

// ToConfluenceStorage converts Markdown into storage-style XHTML that is valid
// for common Confluence page and comment content.
func ToConfluenceStorage(input string) (string, error) {
	normalized := normalizeMarkdown(input)
	if strings.TrimSpace(normalized) == "" {
		return "", nil
	}

	var buf bytes.Buffer
	if err := confluenceMarkdown.Convert([]byte(normalized), &buf); err != nil {
		return "", err
	}

	output := strings.TrimSpace(buf.String())
	if output == "" {
		return "<p>" + stdhtml.EscapeString(strings.TrimSpace(normalized)) + "</p>", nil
	}
	return output, nil
}

// ToJiraWiki converts Markdown into classic Jira wiki markup.
func ToJiraWiki(input string) (string, error) {
	normalized := normalizeMarkdown(input)
	if strings.TrimSpace(normalized) == "" {
		return "", nil
	}

	source := []byte(normalized)
	doc := jiraMarkdown.Parser().Parse(text.NewReader(source))

	renderer := &jiraRenderer{source: source}
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		renderer.renderBlock(child, 0)
	}

	return strings.TrimSpace(strings.TrimRight(renderer.String(), "\n")), nil
}

func normalizeMarkdown(input string) string {
	return strings.ReplaceAll(input, "\r\n", "\n")
}

type jiraRenderer struct {
	source  []byte
	builder strings.Builder
}

func (r *jiraRenderer) String() string {
	return r.builder.String()
}

func (r *jiraRenderer) renderBlock(node ast.Node, listDepth int) {
	switch n := node.(type) {
	case *ast.Heading:
		text := strings.TrimSpace(r.renderInlineChildren(n))
		if text == "" {
			return
		}
		r.writeLine(fmt.Sprintf("h%d. %s", n.Level, text))
		r.writeBlankLine()
	case *ast.Paragraph:
		r.renderParagraph(n, listDepth)
	case *ast.TextBlock:
		r.renderTextBlock(n, listDepth)
	case *ast.Blockquote:
		r.renderBlockquote(n)
	case *ast.FencedCodeBlock:
		r.renderCodeBlock(string(n.Language(r.source)), string(n.Text(r.source)))
	case *ast.CodeBlock:
		r.renderCodeBlock("", string(n.Text(r.source)))
	case *ast.List:
		r.renderList(n, listDepth)
	case *ast.ThematicBreak:
		r.writeLine("----")
		r.writeBlankLine()
	case *extast.Table:
		r.renderTable(n)
	case *ast.HTMLBlock:
		r.writeLine(strings.TrimSpace(string(n.Text(r.source))))
		r.writeBlankLine()
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			r.renderBlock(child, listDepth)
		}
	}
}

func (r *jiraRenderer) renderParagraph(node *ast.Paragraph, listDepth int) {
	text := strings.TrimSpace(r.renderInlineChildren(node))
	if text == "" {
		return
	}
	r.writeLine(text)
	if listDepth == 0 {
		r.writeBlankLine()
	}
}

func (r *jiraRenderer) renderTextBlock(node *ast.TextBlock, listDepth int) {
	text := strings.TrimSpace(r.renderInlineChildren(node))
	if text == "" {
		return
	}
	r.writeLine(text)
	if listDepth == 0 {
		r.writeBlankLine()
	}
}

func (r *jiraRenderer) renderBlockquote(node *ast.Blockquote) {
	content := strings.TrimSpace(r.renderBlockChildren(node, 0))
	if content == "" {
		return
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, " ")
		if line == "" {
			continue
		}
		r.writeLine("bq. " + line)
	}
	r.writeBlankLine()
}

func (r *jiraRenderer) renderCodeBlock(language, code string) {
	language = strings.TrimSpace(language)
	if language != "" {
		r.writeLine("{code:" + language + "}")
	} else {
		r.writeLine("{code}")
	}
	r.builder.WriteString(strings.TrimRight(code, "\n"))
	r.builder.WriteString("\n")
	r.writeLine("{code}")
	r.writeBlankLine()
}

func (r *jiraRenderer) renderList(list *ast.List, listDepth int) {
	index := list.Start
	if index <= 0 {
		index = 1
	}
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		r.renderListItem(item, listDepth+1, list.IsOrdered(), index)
		if list.IsOrdered() {
			index++
		}
	}
	if listDepth == 0 {
		r.writeBlankLine()
	}
}

func (r *jiraRenderer) renderListItem(node ast.Node, depth int, ordered bool, index int) {
	marker := "*"
	if ordered {
		marker = "#"
	}
	prefix := strings.Repeat(marker, depth) + " "

	checkboxPrefix := ""
	firstRenderable := node.FirstChild()
	if task, ok := firstRenderable.(*extast.TaskCheckBox); ok {
		if task.IsChecked {
			checkboxPrefix = "[x] "
		} else {
			checkboxPrefix = "[ ] "
		}
		firstRenderable = task.NextSibling()
	}

	for child := firstRenderable; child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Paragraph:
			line := strings.TrimSpace(r.renderInlineChildren(n))
			if line == "" {
				continue
			}
			r.writeLine(prefix + checkboxPrefix + line)
			checkboxPrefix = ""
		case *ast.TextBlock:
			line := strings.TrimSpace(r.renderInlineChildren(n))
			if line == "" {
				continue
			}
			r.writeLine(prefix + checkboxPrefix + line)
			checkboxPrefix = ""
		case *ast.List:
			r.renderList(n, depth)
		case *ast.FencedCodeBlock:
			r.writeLine(prefix + "{code}")
			r.builder.WriteString(strings.TrimRight(string(n.Text(r.source)), "\n"))
			r.builder.WriteString("\n")
			r.writeLine("{code}")
		default:
			rendered := strings.TrimSpace(r.renderBlockChildren(child, depth))
			if rendered == "" {
				continue
			}
			for _, line := range strings.Split(rendered, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				r.writeLine(prefix + line)
			}
		}
	}
}

func (r *jiraRenderer) renderTable(table *extast.Table) {
	for child := table.FirstChild(); child != nil; child = child.NextSibling() {
		switch child.Kind() {
		case extast.KindTableHeader:
			r.renderTableRow(child, true)
		case extast.KindTableRow:
			r.renderTableRow(child, false)
		}
	}
	r.writeBlankLine()
}

func (r *jiraRenderer) renderTableRow(row ast.Node, header bool) {
	delim := "|"
	if header {
		delim = "||"
	}
	r.builder.WriteString(delim)
	for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
		text := strings.TrimSpace(r.renderInlineChildren(cell))
		if text == "" {
			text = strings.TrimSpace(r.renderBlockChildren(cell, 0))
		}
		text = strings.ReplaceAll(text, "\n", " ")
		r.builder.WriteString(text)
		r.builder.WriteString(delim)
	}
	r.builder.WriteString("\n")
}

func (r *jiraRenderer) renderBlockChildren(node ast.Node, listDepth int) string {
	childRenderer := &jiraRenderer{source: r.source}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		childRenderer.renderBlock(child, listDepth)
	}
	return strings.TrimRight(childRenderer.String(), "\n")
}

func (r *jiraRenderer) renderInlineChildren(node ast.Node) string {
	var builder strings.Builder
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		r.renderInlineNode(&builder, child)
	}
	return builder.String()
}

func (r *jiraRenderer) renderInlineNode(builder *strings.Builder, node ast.Node) {
	switch n := node.(type) {
	case *ast.Text:
		builder.WriteString(escapeJiraText(string(n.Text(r.source))))
		if n.HardLineBreak() || n.SoftLineBreak() {
			builder.WriteString("\n")
		}
	case *ast.String:
		builder.WriteString(escapeJiraText(string(n.Value)))
	case *ast.Emphasis:
		marker := "_"
		if n.Level >= 2 {
			marker = "*"
		}
		builder.WriteString(marker)
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			r.renderInlineNode(builder, child)
		}
		builder.WriteString(marker)
	case *extast.Strikethrough:
		builder.WriteString("-")
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			r.renderInlineNode(builder, child)
		}
		builder.WriteString("-")
	case *ast.CodeSpan:
		builder.WriteString("{{")
		builder.WriteString(strings.TrimSpace(r.renderInlineChildren(n)))
		builder.WriteString("}}")
	case *ast.Link:
		label := strings.TrimSpace(r.renderInlineChildren(n))
		url := strings.TrimSpace(string(n.Destination))
		if label == "" {
			label = url
		}
		builder.WriteString("[")
		builder.WriteString(label)
		builder.WriteString("|")
		builder.WriteString(url)
		builder.WriteString("]")
	case *ast.AutoLink:
		url := strings.TrimSpace(string(n.URL(r.source)))
		label := strings.TrimSpace(string(n.Label(r.source)))
		if label == "" {
			label = url
		}
		builder.WriteString("[")
		builder.WriteString(label)
		builder.WriteString("|")
		builder.WriteString(url)
		builder.WriteString("]")
	case *extast.TaskCheckBox:
		if n.IsChecked {
			builder.WriteString("[x] ")
		} else {
			builder.WriteString("[ ] ")
		}
	case *ast.RawHTML:
		builder.WriteString(string(n.Text(r.source)))
	case *ast.Image:
		alt := strings.TrimSpace(r.renderInlineChildren(n))
		url := strings.TrimSpace(string(n.Destination))
		if alt == "" {
			alt = url
		}
		builder.WriteString("!")
		builder.WriteString(alt)
		builder.WriteString("|")
		builder.WriteString(url)
		builder.WriteString("!")
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			r.renderInlineNode(builder, child)
		}
	}
}

func (r *jiraRenderer) writeLine(line string) {
	r.builder.WriteString(strings.TrimRight(line, " "))
	r.builder.WriteString("\n")
}

func (r *jiraRenderer) writeBlankLine() {
	if !strings.HasSuffix(r.builder.String(), "\n\n") {
		r.builder.WriteString("\n")
	}
}

func escapeJiraText(text string) string {
	replacer := strings.NewReplacer(
		`\\`, `\\\\`,
		`[`, `\[`,
		`]`, `\]`,
		`{`, `\{`,
		`}`, `\}`,
		`|`, `\|`,
	)
	return replacer.Replace(text)
}
