package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/shlex"
	"github.com/spf13/cobra"

	"github.com/notabhay/cojira/internal/config"
	"github.com/notabhay/cojira/internal/dotenv"
)

const maxAliasDepth = 3

// NewRootCmd creates the root cobra command for cojira.
// Callers should add subcommands to it (confluence, jira, meta commands, etc.)
// before calling Execute.
func NewRootCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cojira",
		Short: "Agent-first Confluence + Jira automation",
		Long: `cojira: agent-first Confluence + Jira automation

Usage:
  cojira <tool> [args]

Tools:
  describe     Print machine-readable capabilities (for agents)
  do           Parse natural-language intent into a command
  doctor       Pre-flight config/connectivity checks
  init         Interactive setup wizard for humans
  plan         Preview a command without applying changes
  bootstrap    Merge cojira guidance into AGENTS.md and CLAUDE.md
  confluence   Confluence page management
  jira         Jira issue management

Examples:
  cojira describe --output-mode json
  cojira do 'move PROJ-123 to Done'
  cojira doctor
  cojira bootstrap
  cojira confluence --help
  cojira jira --help`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.TraverseChildren = true
	return cmd
}

// Execute runs the given root command with alias expansion support.
func Execute(rootCmd *cobra.Command) error {
	args := os.Args[1:]
	if len(args) > 0 {
		tool := strings.ToLower(strings.TrimSpace(args[0]))
		if expanded := tryExpandAlias(tool, args[1:], 0); expanded != nil {
			rootCmd.SetArgs(expanded)
			return rootCmd.Execute()
		}
	}
	return rootCmd.Execute()
}

// tryExpandAlias checks if `tool` is a configured alias in .cojira.json.
func tryExpandAlias(tool string, rest []string, depth int) []string {
	if depth >= maxAliasDepth {
		fmt.Fprintln(os.Stderr, "Error: Alias expansion depth exceeded (possible loop)")
		return nil
	}

	builtins := map[string]bool{
		"confluence": true, "jira": true, "bootstrap": true,
		"describe": true, "do": true, "doctor": true,
		"init": true, "plan": true, "help": true,
		"completion": true,
	}
	if builtins[tool] {
		return nil
	}

	dotenv.LoadIfPresent(dotenv.DefaultSearchPaths())
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
		if recursive := tryExpandAlias(nextTool, expandedArgs[1:], depth+1); recursive != nil {
			return recursive
		}
	}

	return expandedArgs
}
