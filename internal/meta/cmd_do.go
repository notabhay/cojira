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
	identRe   = `(?:` + jiraKeyRe + `|` + urlRe + `|\d+)`
)

type intentRule struct {
	name    string
	pattern *regexp.Regexp
	build   func(groups map[string]string) ([]string, error)
}

func compileIntentRule(name, pattern string, build func(groups map[string]string) ([]string, error)) intentRule {
	return intentRule{
		name:    name,
		pattern: regexp.MustCompile(pattern),
		build:   build,
	}
}

var intentRules = []intentRule{
	compileIntentRule("whoami", `(?i)^\s*(?:who\s*am\s*i|whoami)\s*\??\s*$`, func(_ map[string]string) ([]string, error) {
		return []string{"jira", "whoami"}, nil
	}),
	compileIntentRule("status", `(?i)^\s*(?:what'?s\s+the\s+status\s+of|status\s+of)\s+(?P<ident>`+identRe+`)\s*\??\s*$`, func(groups map[string]string) ([]string, error) {
		return []string{"jira", "info", groups["ident"], "--output-mode", "summary"}, nil
	}),
	compileIntentRule("show-details", `(?i)^\s*(?:show(?:\s+me)?\s+details?\s+(?:for|of)|show(?:\s+me)?|get|info)\s+(?P<ident>`+identRe+`)\s*\??\s*$`, func(groups map[string]string) ([]string, error) {
		ident := groups["ident"]
		if looksLikeConfluence(ident) {
			return []string{"confluence", "info", ident, "--output-mode", "summary"}, nil
		}
		return []string{"jira", "info", ident, "--output-mode", "summary"}, nil
	}),
	compileIntentRule("add-label", `(?i)^\s*add\s+label\s+(?P<label>\S+)\s+to\s+(?P<ident>`+identRe+`)\s*$`, func(groups map[string]string) ([]string, error) {
		return []string{"jira", "update", groups["ident"], "--set", "labels+=" + groups["label"], "--dry-run"}, nil
	}),
	compileIntentRule("set-priority", `(?i)^\s*(?:change|set)\s+priority(?:\s+for\s+(?P<ident>`+identRe+`))?\s+(?:to|=)\s+(?P<value>.+?)\s*$`, func(groups map[string]string) ([]string, error) {
		if groups["ident"] == "" {
			return nil, fmt.Errorf("Priority updates need a Jira issue key or URL, for example: change priority for PROJ-123 to High")
		}
		value := strings.Trim(strings.TrimSpace(groups["value"]), `"'`)
		return []string{"jira", "update", groups["ident"], "--set", "priority=" + value, "--dry-run"}, nil
	}),
	compileIntentRule("transition-one", `(?i)^\s*(?:move|transition)\s+(?P<ident>`+identRe+`)\s+to\s+(?P<status>.+?)\s*$`, func(groups map[string]string) ([]string, error) {
		status := strings.Trim(strings.TrimSpace(groups["status"]), `"'`)
		return []string{"jira", "transition", groups["ident"], "--to", status, "--dry-run"}, nil
	}),
	compileIntentRule("transition-bulk-open-bugs", `(?i)^\s*move\s+all\s+open\s+bugs(?:\s+in\s+(?P<project>[A-Za-z][A-Za-z0-9_]*))?\s+to\s+(?P<status>.+?)\s*$`, func(groups map[string]string) ([]string, error) {
		project := strings.TrimSpace(groups["project"])
		if project == "" {
			return nil, fmt.Errorf("Bulk bug transitions need a project key, for example: move all open bugs in FOO to Done")
		}
		status := strings.Trim(strings.TrimSpace(groups["status"]), `"'`)
		jql := fmt.Sprintf("project = %s AND type = Bug AND status != %s", project, status)
		return []string{"jira", "bulk-transition", "--jql", jql, "--to", status, "--dry-run"}, nil
	}),
	compileIntentRule("find-open-bugs", `(?i)^\s*find\s+all\s+open\s+bugs\s+in\s+(?P<project>[A-Za-z][A-Za-z0-9_]*)\s*$`, func(groups map[string]string) ([]string, error) {
		project := groups["project"]
		jql := fmt.Sprintf("project = %s AND type = Bug AND status != Done", project)
		return []string{"jira", "search", jql, "--output-mode", "summary"}, nil
	}),
	compileIntentRule("save-search-results", `(?i)^\s*save\s+search\s+results\s+to(?:\s+a)?\s+file(?:\s+(?P<file>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		file := strings.TrimSpace(groups["file"])
		if file == "" {
			return nil, fmt.Errorf("Saving search results needs both a query and an output file; use the direct search command when you know the JQL")
		}
		return nil, fmt.Errorf("Saving search results requires a JQL query as well as the file path; use the direct search command when you know the query")
	}),
	compileIntentRule("show-board", `(?i)^\s*show\s+me\s+the\s+board(?:\s+(?P<board>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		board := strings.TrimSpace(groups["board"])
		if board == "" {
			return nil, fmt.Errorf("Board inspection needs a board ID or board URL, for example: show me the board 45434")
		}
		return []string{"jira", "board-issues", board, "--output-mode", "summary"}, nil
	}),
	compileIntentRule("show-all-board-issues", `(?i)^\s*show\s+me\s+all\s+issues\s+on\s+the\s+board(?:\s+(?P<board>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		board := strings.TrimSpace(groups["board"])
		if board == "" {
			return nil, fmt.Errorf("Showing all board issues needs a board ID or board URL, for example: show me all issues on the board 45434")
		}
		return []string{"jira", "board-issues", board, "--all", "--output-mode", "summary"}, nil
	}),
	compileIntentRule("create-issue-titled", `(?i)^\s*create\s+a\s+new\s+issue\s+in\s+(?P<project>[A-Za-z][A-Za-z0-9_]*)\s+(?:titled|called)\s+(?P<summary>.+?)\s*$`, func(groups map[string]string) ([]string, error) {
		summary := strings.Trim(strings.TrimSpace(groups["summary"]), `"'`)
		if summary == "" {
			return nil, fmt.Errorf("Issue creation needs a summary, for example: create a new issue in PROJ titled Investigate login bug")
		}
		return []string{"jira", "create", "--project", groups["project"], "--type", "Task", "--summary", summary, "--dry-run"}, nil
	}),
	compileIntentRule("create-issue", `(?i)^\s*create\s+a\s+new\s+issue\s+in\s+(?P<project>[A-Za-z][A-Za-z0-9_]*)\s*$`, func(groups map[string]string) ([]string, error) {
		return nil, fmt.Errorf("Issue creation now supports quick flags. Use a summary too, for example: create a new issue in %s titled Investigate login bug", groups["project"])
	}),
	compileIntentRule("clone-issue", `(?i)^\s*clone\s+(?P<ident>`+identRe+`)\s*$`, func(groups map[string]string) ([]string, error) {
		return []string{"jira", "clone", groups["ident"], "--dry-run"}, nil
	}),
	compileIntentRule("development-summary", `(?i)^\s*(?:show|read)\s+(?:the\s+)?development\s+summary(?:\s+for\s+(?P<ident>`+identRe+`))?\s*$`, func(groups map[string]string) ([]string, error) {
		ident := strings.TrimSpace(groups["ident"])
		if ident == "" {
			return nil, fmt.Errorf("Development summary needs a Jira issue key or URL, for example: show the development summary for PROJ-123")
		}
		return []string{"jira", "--experimental", "development", "summary", ident, "--output-mode", "json"}, nil
	}),
	compileIntentRule("development-pull-requests", `(?i)^\s*(?:show|read)\s+(?:the\s+)?pull\s+requests?(?:\s+for\s+(?P<ident>`+identRe+`))?\s*$`, func(groups map[string]string) ([]string, error) {
		ident := strings.TrimSpace(groups["ident"])
		if ident == "" {
			return nil, fmt.Errorf("Pull request inspection needs a Jira issue key or URL, for example: show the pull requests for PROJ-123")
		}
		return []string{"jira", "--experimental", "development", "pull-requests", ident, "--output-mode", "json"}, nil
	}),
	compileIntentRule("list-transitions", `(?i)^\s*list\s+available\s+transitions(?:\s+for\s+(?P<ident>`+identRe+`))?\s*$`, func(groups map[string]string) ([]string, error) {
		ident := strings.TrimSpace(groups["ident"])
		if ident == "" {
			return nil, fmt.Errorf("Listing transitions needs a Jira issue key or URL, for example: list available transitions for PROJ-123")
		}
		return []string{"jira", "transitions", ident}, nil
	}),
	compileIntentRule("fields", `(?i)^\s*what\s+fields\s+are\s+available(?:\s+(?:for|matching)\s+(?P<query>.+))?\s*\??\s*$`, func(groups map[string]string) ([]string, error) {
		query := strings.Trim(strings.TrimSpace(groups["query"]), `"'`)
		if query == "" {
			return []string{"jira", "fields"}, nil
		}
		return []string{"jira", "fields", "--query", query}, nil
	}),
	compileIntentRule("validate-payload", `(?i)^\s*validate\s+this\s+payload(?:\s+(?P<file>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		file := strings.TrimSpace(groups["file"])
		if file == "" {
			return nil, fmt.Errorf("Payload validation needs a JSON file path, for example: validate this payload payload.json")
		}
		return []string{"jira", "validate", file}, nil
	}),
	compileIntentRule("bulk-rename", `(?i)^\s*rename\s+issues\s+in\s+bulk(?:\s+from\s+(?P<file>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		file := strings.TrimSpace(groups["file"])
		if file == "" {
			return nil, fmt.Errorf("Bulk summary updates need a CSV or JSON mapping file, for example: rename issues in bulk from map.csv")
		}
		return []string{"jira", "bulk-update-summaries", "--file", file, "--dry-run"}, nil
	}),
	compileIntentRule("bulk-update", `(?i)^\s*bulk\s+update\s+issues(?:\s+matching\s+(?P<jql>.+?)\s+using\s+(?P<payload>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		jql := strings.TrimSpace(groups["jql"])
		payload := strings.TrimSpace(groups["payload"])
		if jql == "" || payload == "" {
			return nil, fmt.Errorf("Bulk updates need both a JQL query and a payload file, for example: bulk update issues matching 'project = FOO' using payload.json")
		}
		return []string{"jira", "bulk-update", "--jql", jql, "--payload", payload, "--dry-run"}, nil
	}),
	compileIntentRule("batch", `(?i)^\s*run\s+a\s+batch\s+of\s+operations(?:\s+from\s+(?P<file>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		file := strings.TrimSpace(groups["file"])
		if file == "" {
			return nil, fmt.Errorf("Batch execution needs a config file, for example: run a batch of operations from config.json")
		}
		return []string{"jira", "batch", file, "--dry-run"}, nil
	}),
	compileIntentRule("sync-project", `(?i)^\s*sync\s+issues\s+to\s+disk(?:\s+for\s+(?P<project>[A-Za-z][A-Za-z0-9_]*))?\s*$`, func(groups map[string]string) ([]string, error) {
		project := strings.TrimSpace(groups["project"])
		if project == "" {
			return nil, fmt.Errorf("Issue sync needs a project key, for example: sync issues to disk for PROJ")
		}
		return []string{"jira", "sync", "--project", project}, nil
	}),
	compileIntentRule("sync-from-dir", `(?i)^\s*sync\s+from\s+local\s+folders(?:\s+under\s+(?P<root>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		root := strings.TrimSpace(groups["root"])
		if root == "" {
			return nil, fmt.Errorf("Sync-from-dir needs a root directory, for example: sync from local folders under ./tickets")
		}
		return []string{"jira", "sync-from-dir", "--root", root, "--dry-run"}, nil
	}),
	compileIntentRule("parse-intent", `(?i)^\s*parse\s+this\s+intent(?:\s+(?P<intent>.+))?\s*$`, func(groups map[string]string) ([]string, error) {
		intent := strings.TrimSpace(groups["intent"])
		if intent == "" {
			return nil, fmt.Errorf("Recursive intent parsing needs the nested intent text after 'parse this intent'")
		}
		return []string{"do", intent}, nil
	}),
	compileIntentRule("board-detail-get", `(?i)^\s*what\s+fields\s+are\s+on\s+the\s+board\s+detail\s+view(?:\s+for\s+(?P<board>\S+))?\s*\??\s*$`, func(groups map[string]string) ([]string, error) {
		board := strings.TrimSpace(groups["board"])
		if board == "" {
			return nil, fmt.Errorf("Board detail view inspection needs a board ID or URL, for example: what fields are on the board detail view for 45434")
		}
		return []string{"jira", "--experimental", "board-detail-view", "get", board, "--output-mode", "json"}, nil
	}),
	compileIntentRule("board-detail-search", `(?i)^\s*find\s+a\s+board\s+detail\s+view\s+field\s+id(?:\s+for\s+(?P<board>\S+)\s+matching\s+(?P<query>.+))?\s*$`, func(groups map[string]string) ([]string, error) {
		board := strings.TrimSpace(groups["board"])
		query := strings.Trim(strings.TrimSpace(groups["query"]), `"'`)
		if board == "" || query == "" {
			return nil, fmt.Errorf("Board detail field search needs a board and a query, for example: find a board detail view field id for 45434 matching epic")
		}
		return []string{"jira", "--experimental", "board-detail-view", "search-fields", board, "--query", query, "--output-mode", "json"}, nil
	}),
	compileIntentRule("board-detail-apply", `(?i)^\s*configure\s+the\s+board\s+detail\s+view(?:\s+for\s+(?P<board>\S+)\s+using\s+(?P<file>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		board := strings.TrimSpace(groups["board"])
		file := strings.TrimSpace(groups["file"])
		if board == "" || file == "" {
			return nil, fmt.Errorf("Board detail view apply needs a board and a file, for example: configure the board detail view for 45434 using fields.json")
		}
		return []string{"jira", "--experimental", "board-detail-view", "apply", board, "--file", file, "--dry-run"}, nil
	}),
	compileIntentRule("swimlanes-get", `(?i)^\s*show\s+me\s+the\s+board\s+swimlanes(?:\s+for\s+(?P<board>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		board := strings.TrimSpace(groups["board"])
		if board == "" {
			return nil, fmt.Errorf("Board swimlane inspection needs a board ID or URL, for example: show me the board swimlanes for 45434")
		}
		return []string{"jira", "--experimental", "board-swimlanes", "get", board, "--output-mode", "json"}, nil
	}),
	compileIntentRule("swimlanes-validate", `(?i)^\s*validate\s+swimlane\s+queries(?:\s+for\s+(?P<board>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		board := strings.TrimSpace(groups["board"])
		if board == "" {
			return nil, fmt.Errorf("Swimlane validation needs a board ID or URL, for example: validate swimlane queries for 45434")
		}
		return []string{"jira", "--experimental", "board-swimlanes", "validate", board, "--output-mode", "summary"}, nil
	}),
	compileIntentRule("swimlanes-simulate", `(?i)^\s*simulate\s+swimlane\s+routing(?:\s+for\s+(?P<board>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		board := strings.TrimSpace(groups["board"])
		if board == "" {
			return nil, fmt.Errorf("Swimlane simulation needs a board ID or URL, for example: simulate swimlane routing for 45434")
		}
		return []string{"jira", "--experimental", "board-swimlanes", "simulate", board, "--output-mode", "summary"}, nil
	}),
	compileIntentRule("unsupported-comment", `(?i)^\s*add\s+a\s+comment\s+to\s+(?P<ident>`+identRe+`)\s*$`, func(groups map[string]string) ([]string, error) {
		return nil, fmt.Errorf("Jira comments are not supported yet for %s", groups["ident"])
	}),
	compileIntentRule("read-confluence", `(?i)^\s*read\s+(?:this\s+)?confluence\s+page(?:\s+(?P<ident>.+))?\s*$`, func(groups map[string]string) ([]string, error) {
		ident := strings.Trim(strings.TrimSpace(groups["ident"]), `"'`)
		if ident == "" {
			return nil, fmt.Errorf("Reading a Confluence page needs a page ID, URL, or SPACE:\"Title\" identifier")
		}
		return []string{"confluence", "view", ident, "--format", "text", "--output-mode", "json"}, nil
	}),
	compileIntentRule("update-confluence", `(?i)^\s*update\s+confluence\s+page\s+(?P<ident>\S+)\s+to\s+include\s+(?P<change>.+)\s*$`, func(groups map[string]string) ([]string, error) {
		return nil, fmt.Errorf("Confluence page editing requires fetching and updating storage XHTML; use the direct page workflow for %s", groups["ident"])
	}),
	compileIntentRule("find-confluence-pages", `(?i)^\s*find\s+(?:confluence\s+)?pages?\s+(?:titled|named|called)\s+(?P<title>.+?)\s*$`, func(groups map[string]string) ([]string, error) {
		title := strings.Trim(strings.TrimSpace(groups["title"]), `"'`)
		return []string{"confluence", "find", title, "--output-mode", "summary"}, nil
	}),
	compileIntentRule("copy-confluence-tree", `(?i)^\s*copy\s+(?:this\s+)?confluence\s+tree(?:\s+(?P<page>\S+)\s+(?:to|under)\s+(?P<parent>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		page := strings.TrimSpace(groups["page"])
		parent := strings.TrimSpace(groups["parent"])
		if page == "" || parent == "" {
			return nil, fmt.Errorf("Copy-tree needs both the source page and destination parent, for example: copy this confluence tree 12345 under 67890")
		}
		return []string{"confluence", "copy-tree", page, parent, "--dry-run"}, nil
	}),
	compileIntentRule("archive-confluence-page", `(?i)^\s*archive\s+(?:this\s+)?confluence\s+page(?:\s+(?P<page>\S+)\s+(?:to|under)\s+(?P<parent>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		page := strings.TrimSpace(groups["page"])
		parent := strings.TrimSpace(groups["parent"])
		if page == "" || parent == "" {
			return nil, fmt.Errorf("Archive needs the source page and destination parent, for example: archive this confluence page 12345 under 67890")
		}
		return []string{"confluence", "archive", page, "--to-parent", parent, "--dry-run"}, nil
	}),
	compileIntentRule("create-confluence-page", `(?i)^\s*create\s+a\s+new\s+page(?:\s+titled\s+(?P<title>.+?)\s+in\s+(?P<space>[A-Za-z][A-Za-z0-9_]*)\s+from\s+(?P<file>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		title := strings.Trim(strings.TrimSpace(groups["title"]), `"'`)
		space := strings.TrimSpace(groups["space"])
		file := strings.TrimSpace(groups["file"])
		if title == "" || space == "" || file == "" {
			return nil, fmt.Errorf("Confluence page creation needs a title, space key, and content file, for example: create a new page titled Release Notes in TEAM from content.html")
		}
		return []string{"confluence", "create", title, "-s", space, "-f", file}, nil
	}),
	compileIntentRule("rename-confluence-page", `(?i)^\s*rename\s+(?:this\s+)?page\s+(?P<page>\S+)\s+to\s+(?P<title>.+?)\s*$`, func(groups map[string]string) ([]string, error) {
		page := strings.TrimSpace(groups["page"])
		title := strings.Trim(strings.TrimSpace(groups["title"]), `"'`)
		if page == "" || title == "" {
			return nil, fmt.Errorf("Rename needs a page identifier and a new title, for example: rename this page 12345 to New Title")
		}
		return []string{"confluence", "rename", page, title}, nil
	}),
	compileIntentRule("move-confluence-page", `(?i)^\s*move\s+(?:this\s+)?page\s+(?P<page>\S+)\s+(?:under|to)\s+(?P<parent>\S+)\s*$`, func(groups map[string]string) ([]string, error) {
		page := strings.TrimSpace(groups["page"])
		parent := strings.TrimSpace(groups["parent"])
		if page == "" || parent == "" {
			return nil, fmt.Errorf("Move needs a page identifier and a destination parent, for example: move this page 12345 under 67890")
		}
		return []string{"confluence", "move", page, parent}, nil
	}),
	compileIntentRule("show-page-tree", `(?i)^\s*show\s+the\s+page\s+tree(?:\s+for\s+(?P<page>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		page := strings.TrimSpace(groups["page"])
		if page == "" {
			return nil, fmt.Errorf("Page tree inspection needs a page identifier, for example: show the page tree for 12345")
		}
		return []string{"confluence", "tree", page, "-d", "5"}, nil
	}),
	compileIntentRule("validate-xhtml", `(?i)^\s*validate\s+this\s+xhtml(?:\s+(?P<file>\S+))?\s*$`, func(groups map[string]string) ([]string, error) {
		file := strings.TrimSpace(groups["file"])
		if file == "" {
			return nil, fmt.Errorf("XHTML validation needs a file path, for example: validate this xhtml page.html")
		}
		return []string{"confluence", "validate", file}, nil
	}),
}

// looksLikeConfluence returns true if ident appears to be a Confluence identifier.
func looksLikeConfluence(ident string) bool {
	if regexp.MustCompile(`^\d+$`).MatchString(ident) {
		return true
	}
	lower := strings.ToLower(ident)
	return strings.Contains(lower, "confluence") ||
		strings.Contains(lower, "/wiki/") ||
		strings.Contains(lower, "/pages/")
}

func namedGroups(re *regexp.Regexp, text string) map[string]string {
	matches := re.FindStringSubmatch(text)
	if matches == nil {
		return nil
	}
	names := re.SubexpNames()
	out := make(map[string]string, len(names))
	for idx, name := range names {
		if idx == 0 || name == "" {
			continue
		}
		out[name] = matches[idx]
	}
	return out
}

// parseIntent maps natural-language text to a structured cojira subcommand.
// It returns a specific error when the phrase is recognized but lacks required
// information, and a generic parse error when nothing in the phrasebook fits.
func parseIntent(text string) ([]string, error) {
	t := strings.TrimSpace(text)
	for _, rule := range intentRules {
		groups := namedGroups(rule.pattern, t)
		if groups == nil {
			continue
		}
		return rule.build(groups)
	}
	return nil, fmt.Errorf("Could not parse intent %q. Use one of the documented phrasebook forms or run the direct command.", text)
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
			expanded, err := parseIntent(text)

			if err != nil {
				hint := "Try a documented phrasebook form or use the direct command."
				if cli.IsJSON(cmd) {
					errObj, _ := output.ErrorObj(
						cerrors.OpFailed,
						err.Error(),
						hint,
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
				fmt.Fprintf(os.Stderr, "%s\n", err.Error())
				fmt.Fprintf(os.Stderr, "Hint: %s\n", hint)
				return &exitError{Code: 1}
			}

			rootCmd.SetArgs(expanded)
			return rootCmd.Execute()
		},
	}
	cli.AddOutputFlags(cmd, false)
	return cmd
}
