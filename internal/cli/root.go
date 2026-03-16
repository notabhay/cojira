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
  bootstrap    Write COJIRA-BOOTSTRAP.md and example templates
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
		expanded, err := tryExpandAlias(tool, args[1:], 0)
		if err != nil {
			return err
		}
		if expanded != nil {
			rootCmd.SetArgs(expanded)
			return rootCmd.Execute()
		}
	}
	return rootCmd.Execute()
}

// tryExpandAlias checks if `tool` is a configured alias in .cojira.json.
func tryExpandAlias(tool string, rest []string, depth int) ([]string, error) {
	if depth >= maxAliasDepth {
		return nil, &config.ConfigError{
			Code:        config.CodeConfigInvalid,
			Message:     "Alias expansion depth exceeded (possible loop).",
			UserMessage: "Your .cojira.json aliases look recursive. Fix the alias chain and try again.",
			ExitCode:    2,
		}
	}

	builtins := map[string]bool{
		"confluence": true, "jira": true, "bootstrap": true,
		"describe": true, "do": true, "doctor": true,
		"init": true, "plan": true, "help": true,
		"completion": true,
	}
	if builtins[tool] {
		return nil, nil
	}

	dotenv.LoadIfPresent(dotenv.DefaultSearchPaths())
	projCfg, err := config.LoadProjectConfig(config.DefaultConfigPaths())
	if err != nil {
		return nil, err
	}
	if projCfg == nil {
		return nil, nil
	}

	template := projCfg.GetAlias(tool)
	if template == "" {
		return nil, nil
	}

	expanded, err := shlex.Split(template)
	if err != nil {
		return nil, &config.ConfigError{
			Code:        config.CodeConfigInvalid,
			Message:     fmt.Sprintf("Alias %q has invalid shell syntax: %v", tool, err),
			UserMessage: fmt.Sprintf("Your %s alias %q is malformed.", config.ConfigFilename, tool),
			ExitCode:    2,
		}
	}

	expandedArgs := append(expanded, rest...)

	if len(expandedArgs) > 0 {
		nextTool := strings.ToLower(strings.TrimSpace(expandedArgs[0]))
		recursive, recurErr := tryExpandAlias(nextTool, expandedArgs[1:], depth+1)
		if recurErr != nil {
			return nil, recurErr
		}
		if recursive != nil {
			return recursive, nil
		}
	}

	return expandedArgs, nil
}
