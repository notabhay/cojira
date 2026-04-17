package jira

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/config"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewCreateCmd creates the "create" subcommand.
func NewCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [file]",
		Short: "Create issue from JSON payload or inline flags",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runCreate,
	}
	cmd.Flags().String("project", "", "Project key (defaults to JIRA_PROJECT or jira.default_project when using flags)")
	cmd.Flags().String("template", "", "Template name from jira.templates in .cojira.json")
	cmd.Flags().String("summary", "", "Issue summary")
	cmd.Flags().String("type", "", "Issue type name (defaults to Task when creating from flags)")
	cmd.Flags().String("priority", "", "Priority name")
	cmd.Flags().String("description", "", "Issue description")
	cmd.Flags().String("description-file", "", "Read description from a text file")
	cmd.Flags().String("description-format", "raw", "Description format: raw, markdown, or adf")
	cmd.Flags().String("assignee", "", "Assignee user reference")
	cmd.Flags().String("reporter", "", "Reporter user reference")
	cmd.Flags().String("parent", "", "Parent issue reference")
	cmd.Flags().StringSlice("labels", nil, "Labels to apply")
	cmd.Flags().StringSlice("components", nil, "Component names to apply")
	cmd.Flags().StringSlice("versions", nil, "Affects version names to apply")
	cmd.Flags().StringSlice("fix-versions", nil, "Fix version names to apply")
	cmd.Flags().StringArray("set", nil, "Shorthand field update (repeatable): field=value, field:=<json>, labels+=x, labels-=x")
	cmd.Flags().Bool("dry-run", false, "Preview without creating")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("interactive", false, "Guided interactive issue creation when running in a TTY")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runCreate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	quiet, _ := cmd.Flags().GetBool("quiet")
	interactive, _ := cmd.Flags().GetBool("interactive")

	file := ""
	if len(args) > 0 {
		file = args[0]
	}
	if interactive {
		if file != "" {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--interactive cannot be combined with a payload file.", ExitCode: 2}
		}
		if !output.IsTTY(int(os.Stdin.Fd())) {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--interactive requires a TTY.", ExitCode: 2}
		}
		if err := applyInteractiveCreatePrompts(cmd, client); err != nil {
			return err
		}
	}

	payload, err := buildCreatePayload(cmd, file)
	if err != nil {
		return err
	}

	fields, _ := payload["fields"].(map[string]any)
	summaryText, _ := fields["summary"].(string)
	var project string
	if proj, ok := fields["project"].(map[string]any); ok {
		project = normalizeMaybeString(proj["key"])
		if project == "" {
			project = normalizeMaybeString(proj["id"])
		}
	}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "create",
				map[string]any{"file": file},
				map[string]any{
					"dry_run":     true,
					"summary":     summaryText,
					"project":     project,
					"payload":     payload,
					"idempotency": map[string]any{"key": output.IdempotencyKey("jira.create", payload)},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			detail := ""
			if project != "" {
				detail = fmt.Sprintf(" (project %s)", project)
			}
			summaryPart := ""
			if summaryText != "" {
				summaryPart = fmt.Sprintf(": %s", summaryText)
			}
			fmt.Printf("Would create Jira issue%s%s.\n", detail, summaryPart)
			return nil
		}
		if !quiet {
			r := output.Receipt{OK: true, DryRun: true, Message: "Would create Jira issue"}
			fmt.Println(r.Format())
		}
		return nil
	}

	if idemKey != "" {
		if idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "jira", "create",
					map[string]any{},
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
					nil, nil, "", "", "", nil,
				))
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	result, err := client.CreateIssue(payload)
	if err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, "jira.create")
	}

	key, _ := result["key"].(string)
	issueID, _ := result["id"].(string)
	receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Created issue %s", stringOr(key, issueID))}

	if mode == "json" {
		var issueURL any
		if key != "" {
			issueURL = fmt.Sprintf("%s/browse/%s", client.BaseURL(), key)
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "create",
			map[string]any{},
			map[string]any{
				"key":         key,
				"id":          issueID,
				"url":         issueURL,
				"receipt":     receipt.Format(),
				"idempotency": map[string]any{"key": output.IdempotencyKey("jira.create", payload)},
			},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		summaryPart := ""
		if summaryText != "" {
			summaryPart = fmt.Sprintf(": %s", summaryText)
		}
		fmt.Printf("Created %s%s.\n", stringOr(key, issueID), summaryPart)
		return nil
	}
	if !quiet {
		fmt.Println(receipt.Format())
	}
	return nil
}

func buildCreatePayload(cmd *cobra.Command, file string) (map[string]any, error) {
	var payload map[string]any
	var err error
	if file != "" {
		payload, err = readJSONFile(file)
		if err != nil {
			return nil, err
		}
	} else {
		payload = map[string]any{}
	}

	fields := map[string]any{}
	if existing, ok := payload["fields"].(map[string]any); ok {
		for k, v := range existing {
			fields[k] = v
		}
	}

	if err := applyCreateTemplate(cmd, fields); err != nil {
		return nil, err
	}

	inlineUsed, err := applyCreateFlags(cmd, fields)
	if err != nil {
		return nil, err
	}

	if len(fields) == 0 {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "No create payload provided.",
			ExitCode: 2,
		}
	}

	if inlineUsed {
		if _, ok := fields["project"]; !ok {
			if project := defaultCreateProject(); project != "" {
				fields["project"] = keyedProjectValue(project)
			}
		}
		if _, ok := fields["issuetype"]; !ok {
			fields["issuetype"] = map[string]any{"name": "Task"}
		}
		if strings.TrimSpace(normalizeMaybeString(fields["summary"])) == "" {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "Summary is required when creating from flags.",
				ExitCode: 2,
			}
		}
		if _, ok := fields["project"]; !ok {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "Project key is required when creating from flags.",
				ExitCode: 2,
			}
		}
	}

	payload["fields"] = fields
	if err := normalizeJiraDescriptionField(fields, descriptionFormatFromCmd(cmd), jiraUsesADF()); err != nil {
		return nil, err
	}
	return payload, nil
}

func previewCreateFields(cmd *cobra.Command) (map[string]any, error) {
	fields := map[string]any{}
	if err := applyCreateTemplate(cmd, fields); err != nil {
		return nil, err
	}
	if _, err := applyCreateFlags(cmd, fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func applyInteractiveCreatePrompts(cmd *cobra.Command, client *Client) error {
	fields, err := previewCreateFields(cmd)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(os.Stdin)

	if !cmd.Flags().Changed("project") {
		projects, _ := client.ListProjects()
		project, err := promptCreateProject(reader, projects, projectFieldValue(fields))
		if err != nil {
			return err
		}
		if project != "" {
			if err := cmd.Flags().Set("project", project); err != nil {
				return err
			}
		}
	}

	if !cmd.Flags().Changed("type") {
		projectKey, _ := cmd.Flags().GetString("project")
		issueTypes, _ := client.ListCreateMetaIssueTypes(strings.TrimSpace(projectKey))
		issueType, err := promptCreateIssueType(reader, issueTypes, issueTypeFieldValue(fields))
		if err != nil {
			return err
		}
		if issueType != "" {
			if err := cmd.Flags().Set("type", issueType); err != nil {
				return err
			}
		}
	}

	if !cmd.Flags().Changed("summary") {
		summary, err := promptCreateText(reader, "Summary", normalizeMaybeString(fields["summary"]), true)
		if err != nil {
			return err
		}
		if summary != "" {
			if err := cmd.Flags().Set("summary", summary); err != nil {
				return err
			}
		}
	}

	if !cmd.Flags().Changed("description") && !cmd.Flags().Changed("description-file") {
		description, err := promptCreateText(reader, "Description (optional)", compactWhitespace(normalizeMaybeString(fields["description"])), false)
		if err != nil {
			return err
		}
		if description != "" {
			if err := cmd.Flags().Set("description", description); err != nil {
				return err
			}
		}
	}

	if !cmd.Flags().Changed("assignee") {
		assignee, err := promptCreateText(reader, "Assignee (optional)", userFieldValue(fields["assignee"]), false)
		if err != nil {
			return err
		}
		if assignee != "" {
			if err := cmd.Flags().Set("assignee", assignee); err != nil {
				return err
			}
		}
	}

	return nil
}

func promptCreateProject(reader *bufio.Reader, projects []map[string]any, defaultProject string) (string, error) {
	defaultProject = strings.TrimSpace(defaultProject)
	if defaultProject == "" {
		defaultProject = defaultCreateProject()
	}
	if len(projects) > 0 {
		fmt.Println("Available projects:")
		fmt.Println()
		rows := make([][]string, 0, len(projects))
		limit := len(projects)
		if limit > 10 {
			limit = 10
		}
		for i := 0; i < limit; i++ {
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1),
				normalizeMaybeString(projects[i]["key"]),
				output.Truncate(normalizeMaybeString(projects[i]["name"]), 48),
			})
		}
		fmt.Println(output.TableString([]string{"#", "KEY", "NAME"}, rows))
		if len(projects) > limit {
			fmt.Printf("\nShowing %d of %d projects. You can still type any project key.\n", limit, len(projects))
		}
		fmt.Println()
	}
	value, err := promptCreateText(reader, "Project key", defaultProject, true)
	if err != nil {
		return "", err
	}
	if idx, err := strconv.Atoi(value); err == nil && idx >= 1 && idx <= len(projects) {
		return normalizeMaybeString(projects[idx-1]["key"]), nil
	}
	return strings.TrimSpace(value), nil
}

func promptCreateIssueType(reader *bufio.Reader, issueTypes []map[string]any, defaultType string) (string, error) {
	options := issueTypePromptOptions(issueTypes)
	defaultType = strings.TrimSpace(defaultType)
	if defaultType == "" {
		defaultType = "Task"
	}
	fmt.Println("Issue types:")
	fmt.Println()
	rows := make([][]string, 0, len(options))
	limit := len(options)
	if limit > 12 {
		limit = 12
	}
	for idx, option := range options[:limit] {
		rows = append(rows, []string{fmt.Sprintf("%d", idx+1), option})
	}
	fmt.Println(output.TableString([]string{"#", "TYPE"}, rows))
	if len(options) > limit {
		fmt.Printf("\nShowing %d of %d issue types. You can still type any issue type name.\n", limit, len(options))
	}
	fmt.Println()
	value, err := promptCreateText(reader, "Issue type", defaultType, true)
	if err != nil {
		return "", err
	}
	if idx, err := strconv.Atoi(value); err == nil && idx >= 1 && idx <= len(options) {
		return options[idx-1], nil
	}
	return strings.TrimSpace(value), nil
}

func issueTypePromptOptions(issueTypes []map[string]any) []string {
	if len(issueTypes) == 0 {
		return []string{"Task", "Bug", "Story", "Epic", "Incident"}
	}
	options := make([]string, 0, len(issueTypes))
	for _, item := range issueTypes {
		if name := strings.TrimSpace(normalizeMaybeString(item["name"])); name != "" {
			options = append(options, name)
		}
	}
	if len(options) == 0 {
		return []string{"Task", "Bug", "Story", "Epic", "Incident"}
	}
	return options
}

func promptCreateText(reader *bufio.Reader, label, defaultValue string, required bool) (string, error) {
	defaultValue = strings.TrimSpace(defaultValue)
	for {
		fmt.Print(label)
		if defaultValue != "" {
			fmt.Printf(" [%s]", output.Truncate(defaultValue, 60))
		}
		fmt.Print(": ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		value := strings.TrimSpace(line)
		if value == "" {
			value = defaultValue
		}
		if required && strings.TrimSpace(value) == "" {
			fmt.Println("This value is required.")
			continue
		}
		return value, nil
	}
}

func projectFieldValue(fields map[string]any) string {
	project, _ := fields["project"].(map[string]any)
	if project == nil {
		return defaultCreateProject()
	}
	return stringOr(project["key"], normalizeMaybeString(project["id"]))
}

func issueTypeFieldValue(fields map[string]any) string {
	issueType, _ := fields["issuetype"].(map[string]any)
	if issueType == nil {
		return "Task"
	}
	return stringOr(issueType["name"], normalizeMaybeString(issueType["id"]))
}

func userFieldValue(raw any) string {
	user, _ := raw.(map[string]any)
	if user == nil {
		return ""
	}
	for _, key := range []string{"name", "accountId", "emailAddress"} {
		if value := normalizeMaybeString(user[key]); value != "" {
			return value
		}
	}
	return ""
}

func applyCreateFlags(cmd *cobra.Command, fields map[string]any) (bool, error) {
	summaryFlag, _ := cmd.Flags().GetString("summary")
	projectFlag, _ := cmd.Flags().GetString("project")
	typeFlag, _ := cmd.Flags().GetString("type")
	priorityFlag, _ := cmd.Flags().GetString("priority")
	descriptionFlag, _ := cmd.Flags().GetString("description")
	descriptionFile, _ := cmd.Flags().GetString("description-file")
	assigneeFlag, _ := cmd.Flags().GetString("assignee")
	reporterFlag, _ := cmd.Flags().GetString("reporter")
	parentFlag, _ := cmd.Flags().GetString("parent")
	labelsFlag, _ := cmd.Flags().GetStringSlice("labels")
	componentsFlag, _ := cmd.Flags().GetStringSlice("components")
	versionsFlag, _ := cmd.Flags().GetStringSlice("versions")
	fixVersionsFlag, _ := cmd.Flags().GetStringSlice("fix-versions")
	setExprs, _ := cmd.Flags().GetStringArray("set")

	if descriptionFlag != "" && descriptionFile != "" {
		return false, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Use either --description or --description-file, not both.",
			ExitCode: 2,
		}
	}

	inlineUsed := false

	if strings.TrimSpace(summaryFlag) != "" {
		fields["summary"] = summaryFlag
		inlineUsed = true
	}
	if strings.TrimSpace(projectFlag) != "" {
		fields["project"] = keyedProjectValue(projectFlag)
		inlineUsed = true
	}
	if strings.TrimSpace(typeFlag) != "" {
		fields["issuetype"] = keyedObjectValue(typeFlag, "name")
		inlineUsed = true
	}
	if strings.TrimSpace(priorityFlag) != "" {
		fields["priority"] = keyedObjectValue(priorityFlag, "name")
		inlineUsed = true
	}
	if strings.TrimSpace(descriptionFile) != "" {
		content, err := readTextFile(descriptionFile)
		if err != nil {
			return false, err
		}
		fields["description"] = content
		inlineUsed = true
	}
	if strings.TrimSpace(descriptionFlag) != "" {
		fields["description"] = descriptionFlag
		inlineUsed = true
	}
	if strings.TrimSpace(assigneeFlag) != "" {
		fields["assignee"] = coerceUserFieldValue(assigneeFlag)
		inlineUsed = true
	}
	if strings.TrimSpace(reporterFlag) != "" {
		fields["reporter"] = coerceUserFieldValue(reporterFlag)
		inlineUsed = true
	}
	if strings.TrimSpace(parentFlag) != "" {
		fields["parent"] = keyedParentValue(parentFlag)
		inlineUsed = true
	}
	if len(labelsFlag) > 0 {
		fields["labels"] = normalizeStringSlice(labelsFlag)
		inlineUsed = true
	}
	if len(componentsFlag) > 0 {
		fields["components"] = toNamedObjects(normalizeStringSlice(componentsFlag))
		inlineUsed = true
	}
	if len(versionsFlag) > 0 {
		fields["versions"] = toNamedObjects(normalizeStringSlice(versionsFlag))
		inlineUsed = true
	}
	if len(fixVersionsFlag) > 0 {
		fields["fixVersions"] = toNamedObjects(normalizeStringSlice(fixVersionsFlag))
		inlineUsed = true
	}

	for _, expr := range setExprs {
		field, op, value, err := ParseSetExpr(expr)
		if err != nil {
			return false, err
		}
		if err := applySetOp(field, op, value, fields, mergedFieldState(fields, fields)); err != nil {
			return false, err
		}
		inlineUsed = true
	}

	return inlineUsed, nil
}

func applyCreateTemplate(cmd *cobra.Command, fields map[string]any) error {
	templateName, _ := cmd.Flags().GetString("template")
	templateName = strings.TrimSpace(templateName)
	if templateName == "" {
		return nil
	}
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil {
		return err
	}
	template, ok := jiraTemplateFromConfig(cfg, templateName)
	if !ok {
		return &cerrors.CojiraError{
			Code:     cerrors.IdentUnresolved,
			Message:  fmt.Sprintf("Template %s was not found in jira.templates.", templateName),
			ExitCode: 1,
		}
	}
	if rawFields, ok := template["fields"].(map[string]any); ok {
		for key, value := range rawFields {
			fields[key] = value
		}
	}
	applyTemplateScalar := func(flag string, value any) {
		if _, exists := fields[flag]; exists {
			return
		}
		fields[flag] = value
	}
	if summary := normalizeMaybeString(template["summary"]); summary != "" {
		applyTemplateScalar("summary", summary)
	}
	if project := normalizeMaybeString(template["project"]); project != "" {
		applyTemplateScalar("project", keyedProjectValue(project))
	}
	if issueType := normalizeMaybeString(template["type"]); issueType != "" {
		applyTemplateScalar("issuetype", keyedObjectValue(issueType, "name"))
	}
	if priority := normalizeMaybeString(template["priority"]); priority != "" {
		applyTemplateScalar("priority", keyedObjectValue(priority, "name"))
	}
	if description := normalizeMaybeString(template["description"]); description != "" {
		applyTemplateScalar("description", description)
	}
	if assignee := normalizeMaybeString(template["assignee"]); assignee != "" {
		applyTemplateScalar("assignee", coerceUserFieldValue(assignee))
	}
	if reporter := normalizeMaybeString(template["reporter"]); reporter != "" {
		applyTemplateScalar("reporter", coerceUserFieldValue(reporter))
	}
	if parent := normalizeMaybeString(template["parent"]); parent != "" {
		applyTemplateScalar("parent", keyedParentValue(parent))
	}
	applyTemplateNamedList(fields, "labels", template["labels"], false)
	applyTemplateNamedList(fields, "components", template["components"], true)
	applyTemplateNamedList(fields, "versions", template["versions"], true)
	applyTemplateNamedList(fields, "fixVersions", firstNonNil(template["fixVersions"], template["fix_versions"]), true)
	return nil
}

func applyTemplateNamedList(fields map[string]any, field string, raw any, named bool) {
	if _, exists := fields[field]; exists {
		return
	}
	values := normalizeTemplateStringSlice(raw)
	if len(values) == 0 {
		return
	}
	if named {
		fields[field] = toNamedObjects(values)
		return
	}
	fields[field] = values
}

func normalizeTemplateStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return normalizeStringSlice(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return normalizeStringSlice(parts)
	case string:
		return normalizeStringSlice([]string{value})
	default:
		return nil
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func descriptionFormatFromCmd(cmd *cobra.Command) string {
	format, _ := cmd.Flags().GetString("description-format")
	return format
}

func defaultCreateProject() string {
	if project := strings.TrimSpace(os.Getenv("JIRA_PROJECT")); project != "" {
		return project
	}
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil || cfg == nil {
		return ""
	}
	project, _ := cfg.GetValue([]string{"jira", "default_project"}, "").(string)
	return strings.TrimSpace(project)
}

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func toNamedObjects(values []string) []map[string]any {
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]any{"name": value})
	}
	return out
}
