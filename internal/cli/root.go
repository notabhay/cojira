package cli

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/google/shlex"
	"github.com/spf13/cobra"

	"github.com/notabhay/cojira/internal/config"
	"github.com/notabhay/cojira/internal/dotenv"
)

const maxAliasDepth = 3

var rootExamples = []string{
	"  cojira describe --output-mode json",
	"  cojira auth status",
	"  cojira do 'move PROJ-123 to Done'",
	"  cojira doctor",
	"  cojira events tail --latest",
	"  cojira dry-run record jira transition PROJ-123 --to Done",
	"  cojira apply <plan-id> --yes",
	"  cojira convert --from markdown --to jira-wiki -f note.md",
	"  cojira bootstrap",
	"  cojira confluence --help",
	"  cojira jira --help",
}

// NewRootCmd creates the root cobra command for cojira.
// Callers should add subcommands to it (confluence, jira, meta commands, etc.)
// before calling Execute.
func NewRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "cojira",
		Short:         "Agent-first Confluence + Jira automation",
		Long:          "cojira: agent-first Confluence + Jira automation",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.TraverseChildren = true
	cmd.PersistentFlags().String("profile", "", "Named profile from .cojira.json (falls back to COJIRA_PROFILE or default_profile)")
	cmd.PersistentFlags().Bool("stream", false, "Emit newline-delimited JSON output when supported")
	cmd.PersistentFlags().String("select", "", "Client-side projection for JSON or NDJSON output (for example: result.issues)")
	return cmd
}

// Execute runs the given root command with alias expansion support.
func Execute(rootCmd *cobra.Command) error {
	rootCmd.Long = buildRootLong(rootCmd)
	args := os.Args[1:]
	if len(args) > 0 {
		tool := strings.ToLower(strings.TrimSpace(args[0]))
		if expanded := tryExpandAlias(rootCmd, tool, args[1:], 0); expanded != nil {
			rootCmd.SetArgs(expanded)
			return rootCmd.Execute()
		}
	}
	return rootCmd.Execute()
}

// tryExpandAlias checks if `tool` is a configured alias in .cojira.json.
func tryExpandAlias(rootCmd *cobra.Command, tool string, rest []string, depth int) []string {
	if depth >= maxAliasDepth {
		fmt.Fprintln(os.Stderr, "Error: Alias expansion depth exceeded (possible loop)")
		return nil
	}

	if isBuiltInTool(rootCmd, tool) {
		return nil
	}

	dotenv.LoadDefaultOnce()
	projCfg, err := config.LoadProjectConfig(nil)
	if err != nil || projCfg == nil {
		return nil
	}

	template := projCfg.GetAlias(tool)
	if template == "" {
		return nil
	}

	expanded, err := shlex.Split(template)
	if err != nil {
		return nil
	}

	expandedArgs := append(expanded, rest...)

	if len(expandedArgs) > 0 {
		nextTool := strings.ToLower(strings.TrimSpace(expandedArgs[0]))
		if recursive := tryExpandAlias(rootCmd, nextTool, expandedArgs[1:], depth+1); recursive != nil {
			return recursive
		}
	}

	return expandedArgs
}

func isBuiltInTool(rootCmd *cobra.Command, tool string) bool {
	if tool == "" {
		return false
	}
	for _, cmd := range rootCmd.Commands() {
		if strings.EqualFold(cmd.Name(), tool) {
			return true
		}
		if slices.ContainsFunc(cmd.Aliases, func(alias string) bool {
			return strings.EqualFold(alias, tool)
		}) {
			return true
		}
	}
	return strings.EqualFold(tool, "help")
}

func buildRootLong(rootCmd *cobra.Command) string {
	lines := []string{
		"cojira: agent-first Confluence + Jira automation",
		"",
		"Usage:",
		"  cojira <tool> [args]",
		"",
		"Tools:",
	}
	for _, cmd := range rootCmd.Commands() {
		if !cmd.IsAvailableCommand() || cmd.Hidden {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %-12s %s", cmd.Name(), cmd.Short))
	}
	lines = append(lines, "", "Examples:")
	lines = append(lines, rootExamples...)
	return strings.Join(lines, "\n")
}
