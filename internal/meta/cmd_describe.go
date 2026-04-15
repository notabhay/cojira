package meta

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/dotenv"
	"github.com/notabhay/cojira/internal/output"
	"github.com/notabhay/cojira/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// NewDescribeCmd returns the "cojira describe" command.
// rootCmd is used to introspect the cobra command tree.
func NewDescribeCmd(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "describe",
		Short:         "Describe cojira capabilities for agents (machine-readable) or as an agent prompt snippet",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDescribe(cmd, rootCmd)
		},
	}
	cli.AddOutputFlags(cmd, false)
	cmd.Flags().Bool("with-context", false, "Include live context checks (runs doctor; may call Jira/Confluence)")
	cmd.Flags().Bool("agent-prompt", false, "Output a compact prompt fragment describing how to use cojira safely")
	return cmd
}

// buildCommandManifest recursively builds a manifest dict for a cobra command.
func buildCommandManifest(cmd *cobra.Command) map[string]any {
	manifest := map[string]any{
		"prog":        cmd.CommandPath(),
		"description": cmd.Short,
		"arguments":   buildFlagSpecs(cmd),
		"subcommands": map[string]any{},
	}
	subs := map[string]any{}
	for _, sub := range cmd.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		subs[sub.Name()] = buildCommandManifest(sub)
	}
	manifest["subcommands"] = subs
	return manifest
}

// buildFlagSpecs extracts non-hidden flag specs from a command.
func buildFlagSpecs(cmd *cobra.Command) []map[string]any {
	var specs []map[string]any
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		spec := map[string]any{
			"dest":     f.Name,
			"help":     f.Usage,
			"required": false,
			"default":  f.DefValue,
		}
		if f.Shorthand != "" {
			spec["options"] = []string{"-" + f.Shorthand, "--" + f.Name}
		} else {
			spec["options"] = []string{"--" + f.Name}
		}
		specs = append(specs, spec)
	})
	return specs
}

// buildManifest constructs the full describe manifest from the cobra command tree.
func buildManifest(rootCmd *cobra.Command) map[string]any {
	manifest := map[string]any{
		"name":    "cojira",
		"version": version.Version,
		"principles": []string{
			"Non-interactive by default (exit codes 0/1/2/3; 3=needs user interaction).",
			"Safe-first: use --dry-run for bulk/batch before applying.",
			"Confluence content is storage-format XHTML; never convert to Markdown.",
			"During setup, have the user edit .env manually; do not ask them to paste tokens into chat.",
		},
		"env": map[string]any{
			"confluence": map[string]any{
				"required": []string{"CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN"},
			},
			"jira": map[string]any{
				"required": []string{"JIRA_BASE_URL", "JIRA_API_TOKEN"},
				"optional": []string{
					"JIRA_EMAIL", "JIRA_PROJECT", "JIRA_API_VERSION",
					"JIRA_AUTH_MODE", "JIRA_VERIFY_SSL", "JIRA_USER_AGENT",
				},
			},
		},
		"identifiers": map[string]any{
			"confluence_page": []string{
				"12345",
				"https://confluence.../pages/viewpage.action?pageId=12345",
				"https://confluence.../pages/12345/Title",
				"https://confluence.../display/SPACE/Page+Title",
				"APnAVAE (tiny link code) or https://confluence.../x/APnAVAE",
				`SPACE:"My Page Title"`,
			},
			"jira_issue": []string{
				"PROJ-123",
				"10001",
				"https://jira.../browse/PROJ-123",
				"https://jira.../rest/api/2/issue/PROJ-123",
			},
		},
	}

	// Build parsers section from cobra command tree.
	parsers := map[string]any{}
	for _, sub := range rootCmd.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		parsers[sub.Name()] = buildCommandManifest(sub)
	}
	manifest["parsers"] = parsers

	return manifest
}

// configuredToolsFromEnv checks which tools have the required env vars set.
func configuredToolsFromEnv() []string {
	var tools []string
	if os.Getenv("JIRA_BASE_URL") != "" && os.Getenv("JIRA_API_TOKEN") != "" {
		tools = append(tools, "jira")
	}
	if os.Getenv("CONFLUENCE_BASE_URL") != "" && os.Getenv("CONFLUENCE_API_TOKEN") != "" {
		tools = append(tools, "confluence")
	}
	sort.Strings(tools)
	return tools
}

// buildContext runs doctor checks and returns context information.
func buildContext(rootCmd *cobra.Command) map[string]any {
	checks := runDoctorChecks(cli.RetryConfig{
		Timeout:        10.0,
		Retries:        1,
		RetryBaseDelay: 0.5,
		RetryMaxDelay:  2.0,
		Debug:          false,
	})

	var checksOut []map[string]any
	setupNeeded := false
	currentUser := map[string]any{}

	for _, c := range checks {
		obj := map[string]any{
			"ok":      c.OK,
			"name":    c.Name,
			"details": c.Details,
			"warning": c.Warning,
			"error":   c.Error,
		}
		checksOut = append(checksOut, obj)
		if !c.OK {
			setupNeeded = true
		}
		if c.OK && c.Details != nil {
			if user, ok := c.Details["user"]; ok {
				currentUser[c.Name] = user
			}
		}
	}

	return map[string]any{
		"checks":           checksOut,
		"setup_needed":     setupNeeded,
		"configured_tools": configuredToolsFromEnv(),
		"current_user":     currentUser,
	}
}

// filterManifest removes unconfigured tools from the manifest parsers.
func filterManifest(manifest map[string]any, allowed map[string]bool) map[string]any {
	parsers, _ := manifest["parsers"].(map[string]any)
	if parsers == nil {
		return manifest
	}
	for _, tool := range []string{"jira", "confluence"} {
		if !allowed[tool] {
			delete(parsers, tool)
		}
	}
	return manifest
}

// agentPrompt builds a compact text prompt for system prompts.
func agentPrompt(manifest map[string]any) string {
	envMap, _ := manifest["env"].(map[string]any)
	identifiers, _ := manifest["identifiers"].(map[string]any)
	parsers, _ := manifest["parsers"].(map[string]any)

	jiraCmds := sortedSubcommandNames(parsers, "jira")
	confCmds := sortedSubcommandNames(parsers, "confluence")

	confRequired := envRequired(envMap, "confluence")
	jiraRequired := envRequired(envMap, "jira")
	confPages := identList(identifiers, "confluence_page")
	jiraIssues := identList(identifiers, "jira_issue")

	var lines []string
	lines = append(lines, "You can use `cojira` to automate Jira and Confluence safely.")
	lines = append(lines, "")
	lines = append(lines, "Safety rules:")
	lines = append(lines, "- Never print or paste tokens.")
	lines = append(lines, "- During setup, ask the user to edit `.env` manually. After they confirm, sync credentials to `~/.config/cojira/credentials`.")
	lines = append(lines, "- Confluence page bodies are storage format XHTML; preserve <ac:...> and <ri:...> macros.")
	lines = append(lines, "- Use --dry-run for bulk/batch operations before applying.")
	lines = append(lines, "- Require double confirmation before dangerous actions such as bulk mutations, delete-missing applies, board config changes, archive/copy-tree, global credential edits, or file removals.")
	lines = append(lines, "- Output modes: human, json, summary (one-line), auto (json when not a TTY).")
	lines = append(lines, "- Mutating commands print one-line receipts unless --quiet; use --plan for previews.")
	lines = append(lines, "- You run cojira on the user's behalf. Never show CLI commands, JQL, XHTML, or raw JSON to the user.")
	lines = append(lines, "")
	lines = append(lines, "Required env vars:")
	lines = append(lines, fmt.Sprintf("- Confluence: %s", strings.Join(confRequired, ", ")))
	lines = append(lines, fmt.Sprintf("- Jira: %s", strings.Join(jiraRequired, ", ")))
	lines = append(lines, "")
	lines = append(lines, "Identifier formats:")
	lines = append(lines, "- Confluence page: "+strings.Join(confPages, "; "))
	lines = append(lines, "- Jira issue: "+strings.Join(jiraIssues, "; "))
	lines = append(lines, "")
	lines = append(lines, "Discover capabilities:")
	lines = append(lines, "- `cojira describe --output-mode json` (add --with-context for live checks)")
	lines = append(lines, "- `cojira describe --agent-prompt` (compact text prompt for system prompts)")
	lines = append(lines, "- `cojira doctor` (pre-flight checks)")
	lines = append(lines, "")
	lines = append(lines, "Commands:")
	lines = append(lines, "- Meta: `cojira do <intent>`, `cojira describe`, `cojira doctor`, `cojira plan <tool> <cmd>`")
	lines = append(lines, fmt.Sprintf("- Jira: `cojira jira <cmd> ...` where cmd in {%s}", strings.Join(jiraCmds, ", ")))
	lines = append(lines, fmt.Sprintf("- Confluence: `cojira confluence <cmd> ...` where cmd in {%s}", strings.Join(confCmds, ", ")))
	lines = append(lines, "")
	lines = append(lines, "Intent shortcuts:")
	lines = append(lines, `- Jira transition by status: `+"`"+`cojira jira transition <issue> --to "Done" [--dry-run]`+"`")
	lines = append(lines, "- Jira quick field updates: `cojira jira update <issue> --set summary=... --set labels+=urgent [--dry-run]`")
	lines = append(lines, `- Jira bulk transition: `+"`"+`cojira jira bulk-transition --jql '...' --to "Done" --dry-run`+"`")
	lines = append(lines, "- Confluence copy tree: `cojira confluence copy-tree <page> <parent> --dry-run`")
	lines = append(lines, "- Confluence archive: `cojira confluence archive <page> --to-parent <parent> --dry-run`")
	lines = append(lines, "- Board detail view (experimental): `cojira jira --experimental board-detail-view get <board>`")
	lines = append(lines, "- Board swimlanes (experimental): `cojira jira --experimental board-swimlanes get <board>`")
	lines = append(lines, "- Preview any command: `cojira plan <tool> <cmd> ...`")
	lines = append(lines, "- Defaults: optional `.cojira.json` (default project/space/root page)")
	lines = append(lines, "")
	lines = append(lines, "Not supported (tell the user clearly if asked):")
	lines = append(lines, "- Jira: comments, watchers, issue links, attachments, delete, worklogs, sprints, board columns, filters, dashboards, project admin")
	lines = append(lines, "- Confluence: comments, delete pages, permissions, attachments, labels (dedicated), space admin, page versions, templates, blog posts")
	lines = append(lines, "")
	lines = append(lines, "Common workflows:")
	lines = append(lines, "- Confluence: `cojira confluence info <page> --output-mode json` -> `get` -> edit XHTML -> `update`")
	lines = append(lines, "- Jira: `cojira jira info <issue> --output-mode json` -> `update --dry-run` -> apply")
	return strings.Join(lines, "\n")
}

func sortedSubcommandNames(parsers map[string]any, tool string) []string {
	parser, _ := parsers[tool].(map[string]any)
	if parser == nil {
		return nil
	}
	subs, _ := parser["subcommands"].(map[string]any)
	if subs == nil {
		return nil
	}
	names := make([]string, 0, len(subs))
	for name := range subs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func envRequired(envMap map[string]any, tool string) []string {
	if envMap == nil {
		return nil
	}
	section, _ := envMap[tool].(map[string]any)
	if section == nil {
		return nil
	}
	required, _ := section["required"].([]string)
	return required
}

func identList(identifiers map[string]any, key string) []string {
	if identifiers == nil {
		return nil
	}
	list, _ := identifiers[key].([]string)
	return list
}

func runDescribe(cmd *cobra.Command, rootCmd *cobra.Command) error {
	dotenv.LoadIfPresent(dotenv.DefaultSearchPaths())
	cli.NormalizeOutputMode(cmd)

	manifest := buildManifest(rootCmd)
	withContext, _ := cmd.Flags().GetBool("with-context")
	var ctx map[string]any
	if withContext {
		ctx = buildContext(rootCmd)
		manifest["context"] = ctx
		configured, _ := ctx["configured_tools"].([]string)
		if len(configured) > 0 && len(configured) < 2 {
			allowed := map[string]bool{}
			for _, t := range configured {
				allowed[t] = true
			}
			manifest = filterManifest(manifest, allowed)
		}
	}

	agentPromptFlag, _ := cmd.Flags().GetBool("agent-prompt")
	if agentPromptFlag || !cli.IsJSON(cmd) {
		prompt := agentPrompt(manifest)
		if agentPromptFlag && ctx != nil {
			prompt += "\n\nLive context:\n" + agentPromptContext(ctx)
		}
		fmt.Println(prompt)
		return nil
	}

	env := output.BuildEnvelope(
		true, "cojira", "describe",
		map[string]any{}, manifest,
		nil, nil, "", "", "", nil,
	)
	return output.PrintJSON(env)
}

func agentPromptContext(ctx map[string]any) string {
	if ctx == nil {
		return "- No live context available."
	}

	var lines []string
	setupNeeded, _ := ctx["setup_needed"].(bool)
	lines = append(lines, fmt.Sprintf("- Setup needed: %t", setupNeeded))
	if configured, ok := ctx["configured_tools"].([]string); ok && len(configured) > 0 {
		lines = append(lines, "- Configured tools: "+strings.Join(configured, ", "))
	}
	if currentUser, ok := ctx["current_user"].(map[string]any); ok {
		for name, raw := range currentUser {
			user, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			displayName, _ := user["displayName"].(string)
			if displayName != "" {
				lines = append(lines, fmt.Sprintf("- %s user: %s", name, displayName))
			}
		}
	}
	return strings.Join(lines, "\n")
}
