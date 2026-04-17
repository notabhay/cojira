package confluence

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewPagePropertiesCmd creates the "page-properties" command group.
func NewPagePropertiesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "page-properties",
		Short: "Inspect and report Confluence page content properties",
	}
	cmd.AddCommand(
		newPagePropertiesListCmd(),
		newPagePropertiesGetCmd(),
		newPagePropertiesSetCmd(),
		newPagePropertiesDeleteCmd(),
		newPagePropertiesReportCmd(),
	)
	return cmd
}

func newPagePropertiesListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <page>",
		Short: "List content properties on a page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			pageID, err := ResolvePageID(client, args[0], defaultPageID(loadProjectConfigData()))
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			result, err := client.ListPageProperties(pageID, limit)
			if err != nil {
				return err
			}
			return printPageProperties(mode, "page-properties.list", map[string]any{"page": args[0], "page_id": pageID}, result)
		},
	}
	cmd.Flags().Int("limit", 100, "Maximum properties to fetch")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func newPagePropertiesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <page> <property-id>",
		Short: "Get a page content property by id",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			pageID, err := ResolvePageID(client, args[0], defaultPageID(loadProjectConfigData()))
			if err != nil {
				return err
			}
			result, err := client.GetPageProperty(pageID, args[1])
			if err != nil {
				return err
			}
			return printPageProperties(mode, "page-properties.get", map[string]any{"page": args[0], "page_id": pageID, "property_id": args[1]}, result)
		},
	}
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func newPagePropertiesSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <page> <key>",
		Short: "Create or update a page content property",
		Args:  cobra.ExactArgs(2),
		RunE:  runPagePropertySet,
	}
	cmd.Flags().String("value-json", "", "JSON value for the property")
	cmd.Flags().String("value-file", "", "File containing JSON value for the property")
	cmd.Flags().String("property-id", "", "Existing property id to update")
	cmd.Flags().Int("version", 0, "Explicit property version for updates")
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func newPagePropertiesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <page> <property-id>",
		Short: "Delete a page content property",
		Args:  cobra.ExactArgs(2),
		RunE:  runPagePropertyDelete,
	}
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func newPagePropertiesReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Aggregate page content property keys across a space or CQL query",
		RunE:  runPagePropertiesReport,
	}
	cmd.Flags().String("space", "", "Space key to report across")
	cmd.Flags().String("cql", "", "Explicit CQL query to report across")
	cmd.Flags().String("label", "", "Page Properties macro label to extract from storage XHTML")
	cmd.Flags().Bool("macro-report", false, "Parse Page Properties macros from page bodies instead of content properties")
	cmd.Flags().Int("limit", 25, "Maximum pages to inspect")
	cmd.Flags().Int("property-limit", 100, "Maximum properties to fetch per page")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runPagePropertySet(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	pageID, err := ResolvePageID(client, args[0], defaultPageID(loadProjectConfigData()))
	if err != nil {
		return err
	}
	value, err := readJSONValueFromFlags(cmd)
	if err != nil {
		return err
	}
	propertyID, _ := cmd.Flags().GetString("property-id")
	version, _ := cmd.Flags().GetInt("version")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	payload := map[string]any{"key": args[1], "value": value}
	if propertyID != "" || version > 0 {
		if version <= 0 {
			version = 1
		}
		payload["version"] = map[string]any{"number": version}
	}
	target := map[string]any{"page": args[0], "page_id": pageID, "key": args[1]}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "page-properties.set", target, map[string]any{"dry_run": true, "payload": payload}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "page-properties.set", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	var result map[string]any
	if propertyID != "" {
		result, err = client.UpdatePageProperty(pageID, propertyID, payload)
	} else {
		result, err = client.CreatePageProperty(pageID, payload)
	}
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.page-properties.set %s %s", pageID, args[1]))
	}
	return printPageProperties(mode, "page-properties.set", target, result)
}

func runPagePropertyDelete(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	pageID, err := ResolvePageID(client, args[0], defaultPageID(loadProjectConfigData()))
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	target := map[string]any{"page": args[0], "page_id": pageID, "property_id": args[1]}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "page-properties.delete", target, map[string]any{"dry_run": true, "deleted": false}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "page-properties.delete", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	if err := client.DeletePageProperty(pageID, args[1]); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.page-properties.delete %s %s", pageID, args[1]))
	}
	return printPageProperties(mode, "page-properties.delete", target, map[string]any{"deleted": true})
}

func runPagePropertiesReport(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	spaceKey, _ := cmd.Flags().GetString("space")
	cql, _ := cmd.Flags().GetString("cql")
	label, _ := cmd.Flags().GetString("label")
	macroReport, _ := cmd.Flags().GetBool("macro-report")
	limit, _ := cmd.Flags().GetInt("limit")
	propertyLimit, _ := cmd.Flags().GetInt("property-limit")
	if strings.TrimSpace(cql) == "" {
		if strings.TrimSpace(spaceKey) == "" {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use --space or --cql for page-properties report.", ExitCode: 2}
		}
		cql = fmt.Sprintf(`space="%s" and type=page`, strings.ReplaceAll(spaceKey, `"`, `\"`))
	}
	data, err := client.CQL(cql, limit, 0)
	if err != nil {
		return err
	}
	pages := extractResults(data)
	if macroReport || strings.TrimSpace(label) != "" {
		reportRows := make([]map[string]any, 0, len(pages))
		columnCounts := map[string]int{}
		for _, page := range pages {
			pageID := normalizeMaybeString(page["id"])
			fullPage, err := client.GetPageByID(pageID, "body.storage")
			if err != nil {
				return err
			}
			macros := extractPagePropertiesMacros(getNestedString(fullPage, "body", "storage", "value"), label)
			if len(macros) == 0 {
				continue
			}
			for _, macro := range macros {
				for column := range macro.Properties {
					columnCounts[column]++
				}
				reportRows = append(reportRows, map[string]any{
					"page_id":    pageID,
					"title":      normalizeMaybeString(page["title"]),
					"label":      macro.Label,
					"properties": macro.Properties,
				})
			}
		}
		columns := make([]string, 0, len(columnCounts))
		for column := range columnCounts {
			columns = append(columns, column)
		}
		sort.Strings(columns)
		summary := make([]map[string]any, 0, len(columns))
		for _, column := range columns {
			summary = append(summary, map[string]any{"column": column, "pages": columnCounts[column]})
		}
		result := map[string]any{"pages": reportRows, "summary": summary, "macro_report": true, "label": label}
		return printPageProperties(mode, "page-properties.report", map[string]any{"cql": cql, "space": spaceKey, "label": label}, result)
	}
	counts := map[string]int{}
	reportRows := make([]map[string]any, 0, len(pages))
	for _, page := range pages {
		pageID := normalizeMaybeString(page["id"])
		props, err := client.ListPageProperties(pageID, propertyLimit)
		if err != nil {
			return err
		}
		items := extractResults(props)
		keys := make([]string, 0, len(items))
		for _, item := range items {
			key := normalizeMaybeString(item["key"])
			if key == "" {
				continue
			}
			keys = append(keys, key)
			counts[key]++
		}
		sort.Strings(keys)
		reportRows = append(reportRows, map[string]any{
			"page_id": pageID,
			"title":   normalizeMaybeString(page["title"]),
			"keys":    keys,
			"count":   len(keys),
		})
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	summary := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		summary = append(summary, map[string]any{"key": key, "pages": counts[key]})
	}
	result := map[string]any{"pages": reportRows, "summary": summary}
	return printPageProperties(mode, "page-properties.report", map[string]any{"cql": cql, "space": spaceKey}, result)
}

func readJSONValueFromFlags(cmd *cobra.Command) (any, error) {
	raw, _ := cmd.Flags().GetString("value-json")
	filePath, _ := cmd.Flags().GetString("value-file")
	if raw != "" && filePath != "" {
		return nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --value-json or --value-file, not both.", ExitCode: 2}
	}
	if filePath != "" {
		content, err := readTextFile(filePath)
		if err != nil {
			return nil, err
		}
		raw = content
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Property value JSON is required.", ExitCode: 2}
	}
	var result any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: fmt.Sprintf("Invalid property JSON: %v", err), ExitCode: 1}
	}
	return result, nil
}

func printPageProperties(mode, command string, target map[string]any, result map[string]any) error {
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", command, target, result, nil, nil, "", "", "", nil))
	}
	fmt.Println("Completed page properties operation.")
	return nil
}

type pagePropertiesMacro struct {
	Label      string
	Properties map[string]string
}

var (
	detailsMacroRe = regexp.MustCompile(`(?s)<ac:structured-macro[^>]*ac:name="details"[^>]*>(.*?)</ac:structured-macro>`)
	labelParamRe   = regexp.MustCompile(`(?s)<ac:parameter[^>]*ac:name="label"[^>]*>(.*?)</ac:parameter>`)
	bodyRe         = regexp.MustCompile(`(?s)<ac:rich-text-body[^>]*>(.*?)</ac:rich-text-body>`)
	rowRe          = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	cellRe         = regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
	tagRe          = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRe        = regexp.MustCompile(`\s+`)
)

func extractPagePropertiesMacros(body, label string) []pagePropertiesMacro {
	matches := detailsMacroRe.FindAllStringSubmatch(body, -1)
	out := make([]pagePropertiesMacro, 0, len(matches))
	for _, match := range matches {
		block := match[1]
		foundLabel := ""
		if labelMatch := labelParamRe.FindStringSubmatch(block); len(labelMatch) > 1 {
			foundLabel = normalizeMacroText(labelMatch[1])
		}
		if strings.TrimSpace(label) != "" && !strings.EqualFold(strings.TrimSpace(label), foundLabel) {
			continue
		}
		bodyMatch := bodyRe.FindStringSubmatch(block)
		if len(bodyMatch) < 2 {
			continue
		}
		props := extractPropertiesTable(bodyMatch[1])
		if len(props) == 0 {
			continue
		}
		out = append(out, pagePropertiesMacro{Label: foundLabel, Properties: props})
	}
	return out
}

func extractPropertiesTable(body string) map[string]string {
	props := map[string]string{}
	rows := rowRe.FindAllStringSubmatch(body, -1)
	for _, row := range rows {
		cells := cellRe.FindAllStringSubmatch(row[1], -1)
		if len(cells) < 2 {
			continue
		}
		key := normalizeMacroText(cells[0][1])
		value := normalizeMacroText(cells[1][1])
		if key == "" {
			continue
		}
		props[key] = value
	}
	return props
}

func normalizeMacroText(raw string) string {
	text := tagRe.ReplaceAllString(raw, " ")
	text = html.UnescapeString(text)
	text = strings.TrimSpace(spaceRe.ReplaceAllString(text, " "))
	return text
}
