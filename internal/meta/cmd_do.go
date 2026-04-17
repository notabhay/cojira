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

	whoamiRe            = regexp.MustCompile(`(?i)who\s*am\s*i\b`)
	closeRe             = regexp.MustCompile(`(?i)^(close|resolve|complete|finish)\s+` + identRe + `(?:\s+(?:as|to)\s+(.+))?$`)
	transitionRe        = regexp.MustCompile(`(?i)(?:move|transition)\s+` + identRe + `\s+to\s+(.+)`)
	addLabelRe          = regexp.MustCompile(`(?i)add\s+label\s+(\S+)\s+to\s+` + identRe)
	addPageLabelRe      = regexp.MustCompile(`(?i)add\s+label\s+(\S+)\s+to\s+(?:confluence\s+)?page\s+` + identRe)
	removePageLabelRe   = regexp.MustCompile(`(?i)remove\s+label\s+(\S+)\s+from\s+(?:confluence\s+)?page\s+` + identRe)
	updateSetRe         = regexp.MustCompile(`(?i)update\s+` + identRe + `\s+set\s+(\w+)\s+(?:to|=)\s+(.+)`)
	findPagesRe         = regexp.MustCompile(`(?i)find\s+(?:confluence\s+)?pages?\s+(?:titled|named|called)\s+(.+)`)
	createPageRe        = regexp.MustCompile(`(?i)^create\s+(?:a\s+)?(?:confluence\s+)?page\s+(?:titled|called|named)\s+"([^"]+)"(?:\s+in\s+([A-Za-z][A-Za-z0-9_]+))?(?:\s+under\s+(\S+))?$`)
	createBlogRe        = regexp.MustCompile(`(?i)^create\s+(?:a\s+)?blog\s+post\s+(?:titled|called|named)\s+"([^"]+)"(?:\s+in\s+([A-Za-z][A-Za-z0-9_]+))?$`)
	searchRe            = regexp.MustCompile(`(?i)(?:search|find\s+issues?\s+in)\s+(\w+)\s+(?:for|where)\s+(.+)`)
	showBoardRe         = regexp.MustCompile(`(?i)(?:show|view|get)\s+(?:jira\s+)?board\s+(\S+)`)
	listBoardsRe        = regexp.MustCompile(`(?i)(?:list|show|get)\s+(?:(scrum|kanban)\s+)?boards?\b`)
	assignRe            = regexp.MustCompile(`(?i)assign\s+` + identRe + `\s+to\s+(.+)`)
	confluenceCommentRe = regexp.MustCompile(`(?i)(?:comment\s+on|add\s+(?:a\s+)?comment\s+to)\s+(?:confluence\s+)?page\s+` + identRe + `\s+(.+)`)
	commentRe           = regexp.MustCompile(`(?i)(?:comment\s+on|add\s+(?:a\s+)?comment\s+to)\s+` + identRe + `\s+(.+)`)
	watchRe             = regexp.MustCompile(`(?i)(?:watch|add\s+(?:me|myself)\s+as\s+(?:a\s+)?watcher\s+to)\s+` + identRe + `(?:\s+as\s+(.+))?`)
	unwatchRe           = regexp.MustCompile(`(?i)(?:unwatch|remove\s+(?:me|myself)\s+as\s+(?:a\s+)?watcher\s+from)\s+` + identRe + `(?:\s+as\s+(.+))?`)
	worklogRe           = regexp.MustCompile(`(?i)(?:log|worklog)\s+(.+?)\s+(?:on|to)\s+` + identRe + `(?:\s+comment\s+(.+))?$`)
	historyRe           = regexp.MustCompile(`(?i)(?:show|get|view|list)\s+(?:the\s+)?(?:history|changelog)\s+(?:for\s+)?` + identRe)
	diffRe              = regexp.MustCompile(`(?i)(?:show\s+)?diff\s+(?:for\s+)?` + identRe + `\s+(?:history\s+)?([A-Za-z0-9-]+)(?:\s+to\s+([A-Za-z0-9-]+))?`)
	linkSpecificRe      = regexp.MustCompile(`(?i)^link\s+(` + jiraKeyRe + `)\s+([A-Za-z _-]+)\s+(` + jiraKeyRe + `)$`)
	sprintAddRe         = regexp.MustCompile(`(?i)^add\s+(` + jiraKeyRe + `)\s+to\s+sprint\s+(\d+)$`)
	storyPointsRe       = regexp.MustCompile(`(?i)^set\s+(?:story\s+points|storypoints)\s+(?:on\s+)?(` + jiraKeyRe + `)\s+to\s+([0-9.]+)$`)
	sprintProgressRe    = regexp.MustCompile(`(?i)^show\s+sprint\s+progress\s+for\s+board\s+(\S+)$`)
	blockingRe          = regexp.MustCompile(`(?i)^what'?s\s+blocking\s+(` + jiraKeyRe + `)\??$`)
	reopenRe            = regexp.MustCompile(`(?i)^reopen\s+(` + jiraKeyRe + `)$`)
	bulkCloseSprintRe   = regexp.MustCompile(`(?i)^bulk\s+close\s+all\s+done\s+issues\s+in\s+sprint\s+(\d+)$`)
	cloneRe             = regexp.MustCompile(`(?i)(?:clone|duplicate|copy)\s+` + identRe + `(?:\s+to\s+([A-Za-z][A-Za-z0-9_]+))?`)
	createIssueRe       = regexp.MustCompile(`(?i)^create\s+(?:(?:a|an)\s+)?(?:(bug|task|story|epic|incident)\s+)?(?:in\s+([A-Za-z][A-Za-z0-9_]+)\s+)?(?:issue\s+)?(?:titled|called|named|for)?\s*(.+)$`)
	deleteRe            = regexp.MustCompile(`(?i)^(?:delete|remove)\s+(?:(jira\s+issue|confluence\s+page|page|issue)\s+)?` + identRe + `$`)
	archivePageRe       = regexp.MustCompile(`(?i)^archive\s+(?:confluence\s+)?page\s+` + identRe + `\s+(?:under|to)\s+(\S+)(?:\s+label\s+(\S+))?$`)
	showRe              = regexp.MustCompile(`(?i)(?:show|get|details?\s+(?:for|of)|info)\s+(?:(?:confluence|jira)\s+)?(?:(?:page|issue)\s+)?` + identRe)
	defaultSuggests     = []string{
		"jira info <ISSUE>",
		"jira search '<JQL>'",
		"jira create --project <KEY> --summary \"...\"",
		"confluence find \"<TITLE>\"",
	}
)

func defaultTransitionStatus(verb string) string {
	switch strings.ToLower(strings.TrimSpace(verb)) {
	case "close", "resolve":
		return "Closed"
	case "complete", "finish":
		return "Done"
	default:
		return "Done"
	}
}

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

	if m := closeRe.FindStringSubmatch(t); m != nil {
		status := strings.Trim(strings.TrimSpace(m[3]), `"'`)
		if status == "" {
			status = defaultTransitionStatus(m[1])
		}
		return []string{"jira", "transition", m[2], "--to", status, "--dry-run"}
	}

	// "move/transition ISSUE to STATUS"
	if m := transitionRe.FindStringSubmatch(t); m != nil {
		issue := m[1]
		status := strings.Trim(strings.TrimSpace(m[2]), `"'`)
		return []string{"jira", "transition", issue, "--to", status, "--dry-run"}
	}

	if m := assignRe.FindStringSubmatch(t); m != nil {
		issue := m[1]
		assignee := strings.Trim(strings.TrimSpace(m[2]), `"'`)
		return []string{"jira", "assign", issue, assignee, "--dry-run"}
	}

	if m := confluenceCommentRe.FindStringSubmatch(t); m != nil {
		page := m[1]
		comment := strings.Trim(strings.TrimSpace(m[2]), `"'`)
		return []string{"confluence", "comment", page, "--add", comment, "--dry-run"}
	}

	if m := commentRe.FindStringSubmatch(t); m != nil {
		issue := m[1]
		comment := strings.Trim(strings.TrimSpace(m[2]), `"'`)
		return []string{"jira", "comment", issue, "--add", comment, "--dry-run"}
	}

	if m := watchRe.FindStringSubmatch(t); m != nil {
		issue := m[1]
		user := "me"
		if len(m) > 2 && strings.TrimSpace(m[2]) != "" {
			user = strings.Trim(strings.TrimSpace(m[2]), `"'`)
		}
		return []string{"jira", "watchers", issue, "--add", user, "--dry-run"}
	}

	if m := unwatchRe.FindStringSubmatch(t); m != nil {
		issue := m[1]
		user := "me"
		if len(m) > 2 && strings.TrimSpace(m[2]) != "" {
			user = strings.Trim(strings.TrimSpace(m[2]), `"'`)
		}
		return []string{"jira", "watchers", issue, "--remove", user, "--dry-run"}
	}

	if m := worklogRe.FindStringSubmatch(t); m != nil {
		timeSpent := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		issue := m[2]
		args := []string{"jira", "worklog", issue, "--add", "--time-spent", timeSpent, "--dry-run"}
		if len(m) > 3 && strings.TrimSpace(m[3]) != "" {
			args = append(args, "--comment", strings.Trim(strings.TrimSpace(m[3]), `"'`))
		}
		return args
	}

	if m := linkSpecificRe.FindStringSubmatch(t); m != nil {
		return []string{"jira", "link", m[1], m[3], "--type", strings.TrimSpace(m[2]), "--dry-run"}
	}

	if m := sprintAddRe.FindStringSubmatch(t); m != nil {
		return []string{"jira", "sprint", "add-issues", strings.TrimSpace(m[2]), m[1], "--dry-run"}
	}

	if m := storyPointsRe.FindStringSubmatch(t); m != nil {
		return []string{"jira", "update", m[1], "--set", "Story Points="+strings.TrimSpace(m[2]), "--dry-run"}
	}

	if m := sprintProgressRe.FindStringSubmatch(t); m != nil {
		return []string{"jira", "report", "sprint", strings.TrimSpace(m[1])}
	}

	if m := blockingRe.FindStringSubmatch(t); m != nil {
		return []string{"jira", "blocked", m[1]}
	}

	if m := reopenRe.FindStringSubmatch(t); m != nil {
		return []string{"jira", "transition", m[1], "--to", "Reopened", "--dry-run"}
	}

	if m := bulkCloseSprintRe.FindStringSubmatch(t); m != nil {
		jql := fmt.Sprintf(`sprint = %s AND statusCategory = Done`, strings.TrimSpace(m[1]))
		return []string{"jira", "bulk-transition", "--jql", jql, "--to", "Closed", "--dry-run"}
	}

	if m := historyRe.FindStringSubmatch(t); m != nil {
		return []string{"jira", "history", m[1]}
	}

	if m := diffRe.FindStringSubmatch(t); m != nil {
		args := []string{"jira", "diff", m[1], "--from-history", strings.TrimSpace(m[2])}
		if len(m) > 3 && strings.TrimSpace(m[3]) != "" {
			args = append(args, "--to-history", strings.TrimSpace(m[3]))
		}
		return args
	}

	if m := cloneRe.FindStringSubmatch(t); m != nil {
		args := []string{"jira", "clone", m[1], "--dry-run"}
		if len(m) > 2 && strings.TrimSpace(m[2]) != "" {
			args = append(args, "--project", strings.TrimSpace(m[2]))
		}
		return args
	}

	if m := deleteRe.FindStringSubmatch(t); m != nil {
		hint := strings.ToLower(strings.TrimSpace(m[1]))
		ident := m[2]
		if strings.Contains(hint, "confluence") || hint == "page" || looksLikeConfluence(t, ident) {
			return []string{"confluence", "delete", ident, "--dry-run"}
		}
		return []string{"jira", "delete", ident, "--dry-run"}
	}

	// "add label LABEL to ISSUE"
	if m := addLabelRe.FindStringSubmatch(t); m != nil {
		label := m[1]
		issue := m[2]
		return []string{"jira", "update", issue, "--set", "labels+=" + label, "--dry-run"}
	}

	if m := addPageLabelRe.FindStringSubmatch(t); m != nil {
		label := m[1]
		page := m[2]
		return []string{"confluence", "labels", page, "--add", label, "--plan"}
	}

	if m := removePageLabelRe.FindStringSubmatch(t); m != nil {
		label := m[1]
		page := m[2]
		return []string{"confluence", "labels", page, "--remove", label, "--plan"}
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

	if m := createPageRe.FindStringSubmatch(t); m != nil {
		title := strings.TrimSpace(m[1])
		args := []string{"confluence", "create", title, "--plan"}
		if space := strings.TrimSpace(m[2]); space != "" {
			args = append(args, "--space", space)
		}
		if parent := strings.TrimSpace(m[3]); parent != "" {
			args = append(args, "--parent", parent)
		}
		return args
	}

	if m := createBlogRe.FindStringSubmatch(t); m != nil {
		title := strings.TrimSpace(m[1])
		args := []string{"confluence", "blog", "create", title, "--plan"}
		if space := strings.TrimSpace(m[2]); space != "" {
			args = append(args, "--space", space)
		}
		return args
	}

	if m := createIssueRe.FindStringSubmatch(t); m != nil {
		issueType := strings.Trim(strings.TrimSpace(m[1]), `"'`)
		project := strings.Trim(strings.TrimSpace(m[2]), `"'`)
		summary := strings.Trim(strings.TrimSpace(m[3]), `"'`)
		if summary != "" {
			args := []string{"jira", "create", "--summary", summary, "--dry-run"}
			if project != "" {
				args = append(args, "--project", project)
			}
			if issueType != "" {
				args = append(args, "--type", strings.Title(strings.ToLower(issueType)))
			}
			return args
		}
	}

	// "search PROJECT for QUERY"
	if m := searchRe.FindStringSubmatch(t); m != nil {
		project := m[1]
		query := strings.TrimSpace(m[2])
		return []string{"jira", "search", fmt.Sprintf(`project = %s AND text ~ "%s"`, project, query)}
	}

	if m := showBoardRe.FindStringSubmatch(t); m != nil {
		return []string{"jira", "board-view", m[1], "--all"}
	}

	if m := listBoardsRe.FindStringSubmatch(t); m != nil {
		args := []string{"jira", "boards"}
		if boardType := strings.TrimSpace(m[1]); boardType != "" {
			args = append(args, "--type", strings.ToLower(boardType))
		}
		return args
	}

	if m := archivePageRe.FindStringSubmatch(t); m != nil {
		args := []string{"confluence", "archive", m[1], "--to-parent", strings.TrimSpace(m[2]), "--dry-run"}
		if len(m) > 3 && strings.TrimSpace(m[3]) != "" {
			args = append(args, "--label", strings.TrimSpace(m[3]))
		}
		return args
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

func suggestIntent(text string) []string {
	t := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(t, "create"):
		if strings.Contains(t, "page") || strings.Contains(t, "blog") || strings.Contains(t, "confluence") {
			return []string{
				`confluence create "Title" --space <SPACE>`,
				`confluence blog create "Title" --space <SPACE>`,
			}
		}
		return []string{
			"jira create --project <KEY> --summary \"...\"",
			"jira create payload.json",
		}
	case strings.Contains(t, "clone") || strings.Contains(t, "duplicate") || strings.Contains(t, "copy"):
		return []string{"jira clone <ISSUE> [--project <KEY>]"}
	case strings.Contains(t, "close") || strings.Contains(t, "resolve") || strings.Contains(t, "finish") || strings.Contains(t, "complete"):
		return []string{"jira transition <ISSUE> --to Done --dry-run"}
	case strings.Contains(t, "history") || strings.Contains(t, "changelog"):
		return []string{"jira history <ISSUE>"}
	case strings.Contains(t, "diff"):
		return []string{"jira diff <ISSUE> --from-history <ID> [--to-history <ID>]"}
	case strings.Contains(t, "assign"):
		return []string{"jira assign <ISSUE> <USER>"}
	case strings.Contains(t, "comment"):
		return []string{"jira comment <ISSUE> --add \"...\""}
	case strings.Contains(t, "watch"):
		return []string{"jira watchers <ISSUE> --add me"}
	case strings.Contains(t, "log") || strings.Contains(t, "worklog"):
		return []string{"jira worklog <ISSUE> --add --time-spent 1h"}
	case strings.Contains(t, "board"):
		return []string{
			"jira boards",
			"jira board-view <BOARD> --all",
		}
	case strings.Contains(t, "delete") || strings.Contains(t, "remove"):
		return []string{
			"jira delete <ISSUE> --dry-run",
			"confluence delete <PAGE> --dry-run",
		}
	case strings.Contains(t, "archive"):
		return []string{"confluence archive <PAGE> --to-parent <PAGE> --dry-run"}
	case strings.Contains(t, "page") || strings.Contains(t, "confluence"):
		return []string{
			"confluence find \"<TITLE>\"",
			"confluence info <PAGE>",
			"confluence comment <PAGE> --add \"...\" --dry-run",
		}
	default:
		return defaultSuggests
	}
}

func printIntentSuggestions(suggestions []string) {
	if len(suggestions) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "Closest commands:")
	for _, suggestion := range suggestions {
		fmt.Fprintf(os.Stderr, "  - %s\n", suggestion)
	}
}

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
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
			mode := cli.NormalizeOutputMode(cmd)
			text := strings.Join(args, " ")
			expanded := parseIntent(text)
			suggestOnly, _ := cmd.Flags().GetBool("suggest")

			if suggestOnly {
				suggestions := suggestIntent(text)
				result := map[string]any{
					"intent":      text,
					"matched":     expanded != nil,
					"suggestions": suggestions,
				}
				if expanded != nil {
					result["command"] = expanded
				}
				if cli.IsJSON(cmd) {
					return output.PrintJSON(output.BuildEnvelope(true, "cojira", "do", map[string]any{"intent": text}, result, nil, nil, "", "", "", nil))
				}
				if expanded != nil {
					fmt.Printf("Best match: %s\n", strings.Join(expanded, " "))
					return nil
				}
				printIntentSuggestions(suggestions)
				return nil
			}

			if expanded == nil {
				suggestions := suggestIntent(text)
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
						map[string]any{"suggestions": suggestions}, nil, []any{errObj},
						"", "", "", nil,
					)
					_ = output.PrintJSON(env)
					return &exitError{Code: 1}
				}
				fmt.Fprintf(os.Stderr, "Could not parse intent: %q\n", text)
				printIntentSuggestions(suggestions)
				return &exitError{Code: 1}
			}

			if !containsArg(expanded, "--output-mode") {
				expanded = append(expanded, "--output-mode", mode)
			}
			rootCmd.SetArgs(expanded)
			return rootCmd.Execute()
		},
	}
	cli.AddOutputFlags(cmd, false)
	cmd.Flags().Bool("suggest", false, "Return the closest matching commands without executing them")
	return cmd
}
