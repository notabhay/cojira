package confluence

import (
	"fmt"
	"html"
	"os"
	"regexp"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

var (
	inlineCommentMarkerRe = regexp.MustCompile(`(?s)<ac:inline-comment-marker\b[^>]*\bac:ref=(?:"([^"]+)"|'([^']+)')[^>]*>(.*?)</ac:inline-comment-marker>`)
	htmlTagRe             = regexp.MustCompile(`(?s)<[^>]+>`)
)

// NewCommentsCmd creates the "comments" subcommand.
func NewCommentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comments <page>",
		Short: "List page comments with inline anchor context when available",
		Args:  cobra.ExactArgs(1),
		RunE:  runComments,
	}
	cmd.Flags().Bool("inline-only", false, "Show only inline comments")
	cmd.Flags().Bool("footer-only", false, "Show only footer/page comments")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runComments(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	inlineOnly, _ := cmd.Flags().GetBool("inline-only")
	footerOnly, _ := cmd.Flags().GetBool("footer-only")
	if inlineOnly && footerOnly {
		msg := "Use either --inline-only or --footer-only, not both."
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, msg, "", "", nil)
			ec := 2
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "comments",
				map[string]any{"page": args[0]},
				nil, nil, []any{errObj}, "", "", "", &ec,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: msg, ExitCode: 2}
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "comments",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	rawComments, err := client.GetPageComments(pageID, 100, "body.view,version,history,extensions.inlineProperties,extensions.resolution")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "comments",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error fetching comments for page %s: %v\n", pageID, err)
		return err
	}

	storageAnchors := map[string]string{}
	var warnings []any
	if needsStorageAnchors(rawComments) {
		page, fetchErr := client.GetPageByID(pageID, "body.storage")
		if fetchErr != nil {
			warnings = append(warnings, "Could not fetch page storage for inline anchor context; unresolved anchors will be reported.")
		} else {
			storageAnchors = parseInlineCommentAnchors(getNestedString(page, "body", "storage", "value"))
		}
	}

	processed := make([]map[string]any, 0, len(rawComments))
	inlineCount := 0
	footerCount := 0
	for _, rawComment := range rawComments {
		comment := normalizeComment(rawComment, storageAnchors)
		commentType, _ := comment["type"].(string)
		switch commentType {
		case "inline":
			if footerOnly {
				continue
			}
			inlineCount++
		default:
			if inlineOnly {
				continue
			}
			footerCount++
		}
		processed = append(processed, comment)
	}

	result := map[string]any{
		"page_id":      pageID,
		"total":        len(processed),
		"inline_count": inlineCount,
		"footer_count": footerCount,
		"comments":     processed,
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "comments",
			map[string]any{"page": pageArg, "page_id": pageID},
			result, warnings, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		preview := make([]string, 0, 3)
		for _, comment := range processed {
			label := stringOrAny(comment["type"], "footer")
			author := stringOrAny(comment["author"], "Unknown")
			body := stringOrAny(comment["body_text"], "")
			if len(body) > 60 {
				body = body[:57] + "..."
			}
			preview = append(preview, fmt.Sprintf("%s by %s: %s", label, author, body))
			if len(preview) == 3 {
				break
			}
		}
		if len(preview) > 0 {
			fmt.Printf("Fetched %d comment(s) for page %s (inline: %d, footer: %d). First: %s\n", len(processed), pageID, inlineCount, footerCount, strings.Join(preview, " | "))
		} else {
			fmt.Printf("Fetched %d comment(s) for page %s (inline: %d, footer: %d).\n", len(processed), pageID, inlineCount, footerCount)
		}
		return nil
	}

	fmt.Printf("Page %s comments: %d total (inline: %d, footer: %d)\n", pageID, len(processed), inlineCount, footerCount)
	for _, warning := range warnings {
		fmt.Printf("Warning: %v\n", warning)
	}
	for idx, comment := range processed {
		fmt.Printf("\n[%d] %s comment %v\n", idx+1, strings.Title(stringOrAny(comment["type"], "footer")), comment["id"])
		fmt.Printf("Author:  %s\n", stringOrAny(comment["author"], "Unknown"))
		fmt.Printf("Created: %s\n", stringOrAny(comment["created_at"], "-"))
		if comment["type"] == "inline" {
			anchorText := stringOrAny(comment["anchor_text"], "")
			anchorStatus := stringOrAny(comment["anchor_status"], "")
			if anchorText != "" {
				fmt.Printf("Anchor:  %s (%s)\n", anchorText, anchorStatus)
			} else {
				fmt.Printf("Anchor:  unresolved\n")
			}
		}
		fmt.Printf("Comment: %s\n", stringOrAny(comment["body_text"], ""))
	}
	return nil
}

func needsStorageAnchors(comments []map[string]any) bool {
	for _, comment := range comments {
		if isInlineComment(comment) && extractInlineOriginalSelection(comment) == "" && extractInlineMarkerRef(comment) != "" {
			return true
		}
	}
	return false
}

func normalizeComment(comment map[string]any, anchors map[string]string) map[string]any {
	bodyView := getNestedString(comment, "body", "view", "value")
	commentType := "footer"
	if isInlineComment(comment) {
		commentType = "inline"
	}

	out := map[string]any{
		"id":         comment["id"],
		"type":       commentType,
		"author":     extractCommentAuthor(comment),
		"created_at": extractCommentCreatedAt(comment),
		"updated_at": extractCommentUpdatedAt(comment),
		"body_view":  bodyView,
		"body_text":  stripHTML(bodyView),
	}

	if commentType == "inline" {
		markerRef := extractInlineMarkerRef(comment)
		anchorText := extractInlineOriginalSelection(comment)
		anchorStatus := ""
		if anchorText != "" {
			anchorStatus = "api"
		} else if markerRef != "" {
			if fromStorage := anchors[markerRef]; fromStorage != "" {
				anchorText = fromStorage
				anchorStatus = "storage"
			} else {
				anchorStatus = "unresolved"
			}
		} else {
			anchorStatus = "unresolved"
		}
		out["marker_ref"] = markerRef
		out["anchor_text"] = anchorText
		out["anchor_status"] = anchorStatus
	}

	return out
}

func isInlineComment(comment map[string]any) bool {
	location := getNestedString(comment, "extensions", "location")
	if location == "inline" {
		return true
	}
	return extractInlineMarkerRef(comment) != ""
}

func extractInlineMarkerRef(comment map[string]any) string {
	for _, path := range [][]string{
		{"extensions", "inlineProperties", "markerRef"},
		{"extensions", "inlineProperties", "inlineMarkerRef"},
		{"extensions", "inlineMarkerRef"},
	} {
		if value := getNestedString(comment, path...); value != "" {
			return value
		}
	}
	return ""
}

func extractInlineOriginalSelection(comment map[string]any) string {
	for _, path := range [][]string{
		{"extensions", "inlineProperties", "originalSelection"},
		{"extensions", "inlineProperties", "inlineOriginalSelection"},
		{"extensions", "inlineOriginalSelection"},
	} {
		if value := stripHTML(getNestedString(comment, path...)); value != "" {
			return value
		}
	}
	return ""
}

func extractCommentAuthor(comment map[string]any) string {
	for _, path := range [][]string{
		{"history", "createdBy", "displayName"},
		{"version", "by", "displayName"},
	} {
		if value := getNestedString(comment, path...); value != "" {
			return value
		}
	}
	return ""
}

func extractCommentCreatedAt(comment map[string]any) string {
	for _, path := range [][]string{
		{"history", "createdDate"},
		{"version", "when"},
	} {
		if value := getNestedString(comment, path...); value != "" {
			return value
		}
	}
	return ""
}

func extractCommentUpdatedAt(comment map[string]any) string {
	return getNestedString(comment, "version", "when")
}

func parseInlineCommentAnchors(storage string) map[string]string {
	anchors := map[string]string{}
	for _, match := range inlineCommentMarkerRe.FindAllStringSubmatch(storage, -1) {
		ref := match[1]
		if ref == "" {
			ref = match[2]
		}
		if ref == "" {
			continue
		}
		anchors[ref] = stripHTML(match[3])
	}
	return anchors
}

func stripHTML(value string) string {
	if value == "" {
		return ""
	}
	plain := htmlTagRe.ReplaceAllString(value, " ")
	plain = html.UnescapeString(plain)
	return strings.Join(strings.Fields(plain), " ")
}

func stringOrAny(value any, fallback string) string {
	text, _ := value.(string)
	if text == "" {
		return fallback
	}
	return text
}
