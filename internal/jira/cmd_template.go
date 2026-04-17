package jira

import (
	"fmt"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/config"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewTemplateCmd creates the "template" command group.
func NewTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Inspect Jira create templates stored in .cojira.json",
	}
	cmd.AddCommand(
		newTemplateListCmd(),
		newTemplateShowCmd(),
		newTemplateValidateCmd(),
	)
	return cmd
}

func newTemplateListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available Jira templates",
		Args:  cobra.NoArgs,
		RunE:  runTemplateList,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runTemplateList(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &config.ProjectConfig{Data: map[string]any{}}
	}
	templates := cfg.GetObject([]string{"jira", "templates"})
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "template.list", nil, map[string]any{"templates": names}, nil, nil, "", "", "", nil))
	}
	if len(names) == 0 {
		fmt.Println("No Jira templates.")
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Found %d Jira template(s).\n", len(names))
		return nil
	}
	fmt.Println("Jira templates:")
	fmt.Println()
	for _, name := range names {
		fmt.Printf("  - %s\n", name)
	}
	return nil
}

func newTemplateShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a Jira template",
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplateShow,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runTemplateShow(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &config.ProjectConfig{Data: map[string]any{}}
	}
	template, ok := jiraTemplateFromConfig(cfg, args[0])
	if !ok {
		return &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("Template %s was not found.", args[0]), ExitCode: 1}
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "template.show", map[string]any{"name": args[0]}, template, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Template %s loaded.\n", args[0])
		return nil
	}
	fmt.Printf("Template %s:\n%v\n", args[0], template)
	return nil
}

func newTemplateValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [name]",
		Short: "Validate Jira template shape",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runTemplateValidate,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runTemplateValidate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &config.ProjectConfig{Data: map[string]any{}}
	}
	templates := cfg.GetObject([]string{"jira", "templates"})
	names := make([]string, 0, len(templates))
	if len(args) > 0 {
		names = append(names, args[0])
	} else {
		for name := range templates {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	results := make([]map[string]any, 0, len(names))
	for _, name := range names {
		template, ok := jiraTemplateFromConfig(cfg, name)
		if !ok {
			results = append(results, map[string]any{"name": name, "ok": false, "errors": []string{"template not found"}})
			continue
		}
		errors := validateTemplateObject(template)
		results = append(results, map[string]any{"name": name, "ok": len(errors) == 0, "errors": errors})
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "template.validate", nil, map[string]any{"results": results}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		okCount := 0
		for _, item := range results {
			if item["ok"] == true {
				okCount++
			}
		}
		fmt.Printf("%d of %d template(s) validated cleanly.\n", okCount, len(results))
		return nil
	}
	for _, item := range results {
		fmt.Printf("%s: ok=%v\n", item["name"], item["ok"])
		if errs, ok := item["errors"].([]string); ok && len(errs) > 0 {
			for _, msg := range errs {
				fmt.Printf("  - %s\n", msg)
			}
		}
	}
	return nil
}

func validateTemplateObject(template map[string]any) []string {
	var errors []string
	if rawFields, ok := template["fields"]; ok {
		if _, ok := rawFields.(map[string]any); !ok {
			errors = append(errors, "fields must be an object")
		}
	}
	for _, key := range []string{"summary", "project", "type", "priority", "description", "assignee", "reporter", "parent"} {
		if value, ok := template[key]; ok {
			if _, ok := value.(string); !ok {
				errors = append(errors, fmt.Sprintf("%s must be a string", key))
			}
		}
	}
	for _, key := range []string{"labels", "components", "versions", "fixVersions", "fix_versions"} {
		if value, ok := template[key]; ok {
			switch value.(type) {
			case []string, []any, string:
			default:
				errors = append(errors, fmt.Sprintf("%s must be a string or string list", key))
			}
		}
	}
	if _, ok := template["summary"]; !ok {
		if _, ok := template["fields"].(map[string]any); !ok {
			errors = append(errors, "template should define summary or fields.summary")
		} else {
			fields := template["fields"].(map[string]any)
			if strings.TrimSpace(normalizeMaybeString(fields["summary"])) == "" {
				errors = append(errors, "template should define summary or fields.summary")
			}
		}
	}
	return errors
}
