package meta

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

var (
	jiraKeyRe = `[A-Za-z][A-Za-z0-9_]+-\d+`
	urlRe     = `https?://\S+`
	identRe   = `(` + jiraKeyRe + `|` + urlRe + `|\d+)`

	whoamiRe     = regexp.MustCompile(`(?i)who\s*am\s*i\b`)
	transitionRe = regexp.MustCompile(`(?i)(?:move|transition)\s+` + identRe + `\s+to\s+(.+)`)
	addLabelRe   = regexp.MustCompile(`(?i)add\s+label\s+(\S+)\s+to\s+` + identRe)
	updateSetRe  = regexp.MustCompile(`(?i)update\s+` + identRe + `\s+set\s+(\w+)\s+(?:to|=)\s+(.+)`)
	findPagesRe  = regexp.MustCompile(`(?i)find\s+(?:confluence\s+)?pages?\s+(?:titled|named|called)\s+(.+)`)
	searchRe     = regexp.MustCompile(`(?i)(?:search|find\s+issues?\s+in)\s+(\w+)\s+(?:for|where)\s+(.+)`)
	showRe       = regexp.MustCompile(`(?i)(?:show|get|details?\s+(?:for|of)|info)\s+(?:(?:confluence|jira)\s+)?(?:(?:page|issue)\s+)?` + identRe)
)

// looksLikeConfluence returns true if ident appears to be a Confluence identifier.
func looksLikeConfluence(fullText string, ident string) bool {
	lower := strings.ToLower(ident)
	if strings.Contains(lower, "confluence") ||
		strings.Contains(lower, "/wiki/") ||
		strings.Contains(lower, "/pages/") {
		return true
	}
	if regexp.MustCompile(`^\d+$`).MatchString(ident) {
		text := strings.ToLower(fullText)
		return strings.Contains(text, "confluence") || strings.Contains(text, "page")
	}
	return false
}

// parseIntent maps natural language text to a cojira subcommand argument list.
// Returns nil if the intent cannot be parsed.
func parseIntent(text string) []string {
	t := strings.TrimSpace(text)

	// "who am i" / "whoami"
	if whoamiRe.MatchString(t) {
		return []string{"jira", "whoami"}
	}

	// "move/transition ISSUE to STATUS"
	if m := transitionRe.FindStringSubmatch(t); m != nil {
		issue := m[1]
		status := strings.Trim(strings.TrimSpace(m[2]), `"'`)
		return []string{"jira", "transition", issue, "--to", status, "--dry-run"}
	}

	// "add label LABEL to ISSUE"
	if m := addLabelRe.FindStringSubmatch(t); m != nil {
		label := m[1]
		issue := m[2]
		return []string{"jira", "update", issue, "--set", "labels+=" + label, "--dry-run"}
	}

	// "update ISSUE set FIELD to VALUE"
	if m := updateSetRe.FindStringSubmatch(t); m != nil {
		issue := m[1]
		field := m[2]
		value := strings.TrimSpace(m[3])
		return []string{"jira", "update", issue, "--set", field + "=" + value, "--dry-run"}
	}

	// "find pages titled TITLE"
	if m := findPagesRe.FindStringSubmatch(t); m != nil {
		title := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		return []string{"confluence", "find", title}
	}

	// "search PROJECT for QUERY"
	if m := searchRe.FindStringSubmatch(t); m != nil {
		project := m[1]
		query := strings.TrimSpace(m[2])
		return []string{"jira", "search", fmt.Sprintf(`project = %s AND text ~ "%s"`, project, query)}
	}

	// "show/get/info IDENTIFIER"
	if m := showRe.FindStringSubmatch(t); m != nil {
		ident := m[1]
		tool := "jira"
		if looksLikeConfluence(t, ident) {
			tool = "confluence"
		}
		return []string{tool, "info", ident}
	}

	return nil
}

// NewDoCmd returns the "cojira do" command which parses natural-language
// intent and dispatches to the right subcommand.
func NewDoCmd(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "do <intent>",
		Short:         "Parse natural-language intent and map to the right subcommand",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.NormalizeOutputMode(cmd)
			text := strings.Join(args, " ")
			expanded := parseIntent(text)

			if expanded == nil {
				if cli.IsJSON(cmd) {
					errObj, _ := output.ErrorObj(
						cerrors.OpFailed,
						fmt.Sprintf("Could not parse intent: %s", text),
						"Try a more specific phrasing or use the direct command.",
						"", nil,
					)
					env := output.BuildEnvelope(
						false, "cojira", "do",
						map[string]any{"intent": text},
						nil, nil, []any{errObj},
						"", "", "", nil,
					)
					_ = output.PrintJSON(env)
					return &exitError{Code: 1}
				}
				fmt.Fprintf(os.Stderr, "Could not parse intent: %q\n", text)
				fmt.Fprintln(os.Stderr, "Hint: Try 'cojira do move PROJ-123 to Done' or use the direct command.")
				return &exitError{Code: 1}
			}

			rootCmd.SetArgs(expanded)
			return rootCmd.Execute()
		},
	}
	cli.AddOutputFlags(cmd, false)
	return cmd
}
