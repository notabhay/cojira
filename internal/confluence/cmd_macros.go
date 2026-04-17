package confluence

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

type macroSpec struct {
	Name         string
	Description  string
	BodyRequired bool
	Params       []string
}

var confluenceMacroCatalog = []macroSpec{
	{Name: "info", Description: "Information panel", BodyRequired: true},
	{Name: "note", Description: "Note panel", BodyRequired: true},
	{Name: "warning", Description: "Warning panel", BodyRequired: true},
	{Name: "panel", Description: "Panel macro", BodyRequired: true, Params: []string{"title"}},
	{Name: "toc", Description: "Table of contents"},
	{Name: "excerpt", Description: "Excerpt block", BodyRequired: true},
	{Name: "include", Description: "Excerpt include macro", Params: []string{"page"}},
	{Name: "jira", Description: "Jira issue macro", Params: []string{"key"}},
	{Name: "details", Description: "Expandable details block", BodyRequired: true, Params: []string{"title"}},
	{Name: "status", Description: "Status lozenge", Params: []string{"title", "color"}},
}

// NewMacrosCmd creates the "macros" command group.
func NewMacrosCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "macros",
		Short: "Render or insert common Confluence storage macros",
	}
	cmd.AddCommand(newMacrosListCmd(), newMacrosRenderCmd(), newMacrosInsertCmd())
	return cmd
}

func newMacrosListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List supported Confluence macros",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			items := make([]map[string]any, 0, len(confluenceMacroCatalog))
			for _, spec := range confluenceMacroCatalog {
				items = append(items, map[string]any{
					"name":          spec.Name,
					"description":   spec.Description,
					"body_required": spec.BodyRequired,
					"params":        spec.Params,
				})
			}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "macros.list", nil, map[string]any{"macros": items}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found %d supported Confluence macro(s).\n", len(items))
				return nil
			}
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{
					normalizeMaybeString(item["name"]),
					output.Truncate(normalizeMaybeString(item["description"]), 32),
					output.Truncate(strings.Join(toStrings(item["params"]), ","), 20),
				})
			}
			fmt.Println(output.TableString([]string{"NAME", "DESCRIPTION", "PARAMS"}, rows))
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newMacrosRenderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render <macro>",
		Short: "Render a Confluence macro as storage XHTML",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			rendered, spec, err := renderMacroFromCmd(cmd, args[0])
			if err != nil {
				return err
			}
			result := map[string]any{"macro": spec.Name, "content": rendered}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "macros.render", map[string]any{"macro": spec.Name}, result, nil, nil, "", "", "", nil))
			}
			fmt.Fprintln(cmd.OutOrStdout(), rendered)
			return nil
		},
	}
	addMacroFlags(cmd, false)
	return cmd
}

func newMacrosInsertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "insert <page> <macro>",
		Short: "Insert a rendered Confluence macro into a page body",
		Args:  cobra.ExactArgs(2),
		RunE:  runMacrosInsert,
	}
	addMacroFlags(cmd, true)
	cmd.Flags().String("placement", "append", "Where to place the macro: append, prepend, replace-marker")
	cmd.Flags().String("marker", "", "Marker text to replace when --placement=replace-marker")
	cmd.Flags().Bool("minor", false, "Mark the page update as a minor edit")
	cmd.Flags().Bool("dry-run", false, "Preview the insertion without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func addMacroFlags(cmd *cobra.Command, includeHTTP bool) {
	cmd.Flags().String("body", "", "Macro body content")
	cmd.Flags().String("file", "", "Read macro body content from a file")
	cmd.Flags().String("format", "storage", "Body format: storage or markdown")
	cmd.Flags().StringToString("param", nil, "Macro parameter values (repeatable key=value)")
	cli.AddOutputFlags(cmd, true)
	if includeHTTP {
		cli.AddHTTPRetryFlags(cmd)
	}
}

func runMacrosInsert(cmd *cobra.Command, args []string) error {
	cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		return err
	}
	rendered, spec, err := renderMacroFromCmd(cmd, args[1])
	if err != nil {
		return err
	}
	placement, _ := cmd.Flags().GetString("placement")
	marker, _ := cmd.Flags().GetString("marker")
	minorEdit, _ := cmd.Flags().GetBool("minor")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	page, err := client.GetPageByID(pageID, "version,body.storage")
	if err != nil {
		return err
	}
	currentBody := getNestedString(page, "body", "storage", "value")
	newBody, err := insertRenderedMacro(currentBody, rendered, placement, marker)
	if err != nil {
		return err
	}
	target := map[string]any{"page": pageArg, "page_id": pageID, "macro": spec.Name}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "macros.insert", target, map[string]any{"dry_run": true, "content": newBody}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "macros.insert", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}

	payload := map[string]any{
		"type":  "page",
		"title": normalizeMaybeString(page["title"]),
		"version": map[string]any{
			"number":    int(getNestedFloat(page, "version", "number")) + 1,
			"minorEdit": minorEdit,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          newBody,
				"representation": "storage",
			},
		},
	}
	result, err := client.UpdatePage(pageID, payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.macros.insert %s %s", pageID, spec.Name))
	}
	return output.PrintJSON(output.BuildEnvelope(true, "confluence", "macros.insert", target, map[string]any{"page": result, "macro": spec.Name}, nil, nil, "", "", "", nil))
}

func renderMacroFromCmd(cmd *cobra.Command, macroName string) (string, macroSpec, error) {
	spec, err := findMacroSpec(macroName)
	if err != nil {
		return "", macroSpec{}, err
	}
	body, _ := cmd.Flags().GetString("body")
	filePath, _ := cmd.Flags().GetString("file")
	format, _ := cmd.Flags().GetString("format")
	params, _ := cmd.Flags().GetStringToString("param")
	if body != "" && filePath != "" {
		return "", macroSpec{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --body or --file, not both.", ExitCode: 2}
	}
	if filePath != "" {
		content, err := readTextFile(filePath)
		if err != nil {
			return "", macroSpec{}, err
		}
		body = content
	}
	if spec.BodyRequired {
		body = strings.TrimSpace(body)
		if body == "" {
			return "", macroSpec{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Macro %s requires body content.", spec.Name), ExitCode: 2}
		}
		converted, err := convertStorageBody(body, format)
		if err != nil {
			return "", macroSpec{}, err
		}
		body = converted
	}
	return renderMacro(spec, body, params)
}

func findMacroSpec(name string) (macroSpec, error) {
	needle := strings.ToLower(strings.TrimSpace(name))
	for _, spec := range confluenceMacroCatalog {
		if spec.Name == needle {
			return spec, nil
		}
	}
	return macroSpec{}, &cerrors.CojiraError{
		Code:     cerrors.OpFailed,
		Message:  fmt.Sprintf("Unsupported macro %q.", name),
		ExitCode: 2,
	}
}

func renderMacro(spec macroSpec, body string, params map[string]string) (string, macroSpec, error) {
	params = normalizeMacroParams(params)
	for _, key := range spec.Params {
		if strings.TrimSpace(params[key]) == "" {
			return "", macroSpec{}, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Macro %s requires parameter %q.", spec.Name, key),
				ExitCode: 2,
			}
		}
	}
	switch spec.Name {
	case "info", "note", "warning":
		return fmt.Sprintf(`<ac:structured-macro ac:name="%s"><ac:rich-text-body>%s</ac:rich-text-body></ac:structured-macro>`, spec.Name, body), spec, nil
	case "panel":
		var b strings.Builder
		b.WriteString(`<ac:structured-macro ac:name="panel">`)
		if title := strings.TrimSpace(params["title"]); title != "" {
			b.WriteString(fmt.Sprintf(`<ac:parameter ac:name="title">%s</ac:parameter>`, html.EscapeString(title)))
		}
		b.WriteString(`<ac:rich-text-body>`)
		b.WriteString(body)
		b.WriteString(`</ac:rich-text-body></ac:structured-macro>`)
		return b.String(), spec, nil
	case "toc":
		return `<ac:structured-macro ac:name="toc"></ac:structured-macro>`, spec, nil
	case "excerpt":
		return fmt.Sprintf(`<ac:structured-macro ac:name="excerpt"><ac:rich-text-body>%s</ac:rich-text-body></ac:structured-macro>`, body), spec, nil
	case "include":
		return fmt.Sprintf(`<ac:structured-macro ac:name="excerpt-include"><ac:parameter ac:name="">%s</ac:parameter></ac:structured-macro>`, html.EscapeString(params["page"])), spec, nil
	case "jira":
		return fmt.Sprintf(`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">%s</ac:parameter></ac:structured-macro>`, html.EscapeString(params["key"])), spec, nil
	case "details":
		return fmt.Sprintf(`<ac:structured-macro ac:name="details"><ac:parameter ac:name="title">%s</ac:parameter><ac:rich-text-body>%s</ac:rich-text-body></ac:structured-macro>`, html.EscapeString(params["title"]), body), spec, nil
	case "status":
		color := params["color"]
		if color == "" {
			color = params["colour"]
		}
		return fmt.Sprintf(`<ac:structured-macro ac:name="status"><ac:parameter ac:name="title">%s</ac:parameter><ac:parameter ac:name="colour">%s</ac:parameter></ac:structured-macro>`, html.EscapeString(params["title"]), html.EscapeString(color)), spec, nil
	default:
		return "", macroSpec{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Macro %s is not yet renderable.", spec.Name), ExitCode: 2}
	}
}

func normalizeMacroParams(params map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range params {
		out[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return out
}

func insertRenderedMacro(currentBody, rendered, placement, marker string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(placement)) {
	case "", "append":
		return currentBody + rendered, nil
	case "prepend":
		return rendered + currentBody, nil
	case "replace-marker":
		if strings.TrimSpace(marker) == "" {
			return "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--marker is required when --placement=replace-marker.", ExitCode: 2}
		}
		if !strings.Contains(currentBody, marker) {
			return "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Marker text was not found in the page body.", ExitCode: 1}
		}
		return strings.Replace(currentBody, marker, rendered, 1), nil
	default:
		return "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsupported placement %q.", placement), ExitCode: 2}
	}
}

func toStrings(v any) []string {
	raw, _ := v.([]string)
	if raw != nil {
		return raw
	}
	if items, ok := v.([]any); ok {
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, normalizeMaybeString(item))
		}
		sort.Strings(out)
		return out
	}
	return nil
}
