package jira

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/config"
	cerrors "github.com/notabhay/cojira/internal/errors"
)

type createInput struct {
	File            string
	UseStdin        bool
	InlineJSON      string
	TemplateFile    string
	TemplateVars    []string
	CloneIssue      string
	CloneMode       string
	IncludeFields   []string
	ExcludeFields   []string
	BaseDir         string
	Project         string
	IssueType       string
	Summary         string
	Description     string
	DescriptionFile string
	Priority        string
	Parent          string
	Assignee        string
	Components      []string
	Labels          []string
	SetExprs        []string
}

type createResolution struct {
	Payload      map[string]any
	SourceTarget map[string]any
	Primary      string
	Summary      string
	Project      string
}

var portableCloneFields = map[string]bool{
	"summary":     true,
	"description": true,
	"issuetype":   true,
	"priority":    true,
	"labels":      true,
	"components":  true,
	"fixVersions": true,
	"versions":    true,
	"project":     true,
}

var cloneDropFields = map[string]bool{
	"attachment":                    true,
	"comment":                       true,
	"created":                       true,
	"creator":                       true,
	"customfield_27900":             true,
	"lastViewed":                    true,
	"progress":                      true,
	"reporter":                      true,
	"resolution":                    true,
	"resolutiondate":                true,
	"status":                        true,
	"subtasks":                      true,
	"thumbnail":                     true,
	"timeestimate":                  true,
	"timeoriginalestimate":          true,
	"timespent":                     true,
	"updated":                       true,
	"votes":                         true,
	"watches":                       true,
	"worklog":                       true,
	"workratio":                     true,
	"aggregatetimeestimate":         true,
	"aggregatetimeoriginalestimate": true,
	"aggregatetimespent":            true,
}

func usesBasicJiraAuth() bool {
	if strings.TrimSpace(os.Getenv("JIRA_EMAIL")) != "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("JIRA_AUTH_MODE")), "basic")
}

func resolveDefaultProjectKey() string {
	cfg, err := config.LoadProjectConfig(nil)
	if err == nil && cfg != nil {
		if dp, ok := cfg.GetValue([]string{"jira", "default_project"}, "").(string); ok && strings.TrimSpace(dp) != "" {
			return strings.TrimSpace(dp)
		}
	}
	return strings.TrimSpace(os.Getenv("JIRA_PROJECT"))
}

func parseJSONMapBytes(data []byte, label string) (map[string]any, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  fmt.Sprintf("Refusing to load empty JSON from %s.", label),
			ExitCode: 1,
		}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  fmt.Sprintf("Invalid JSON in %s: %v", label, err),
			ExitCode: 1,
		}
	}
	return result, nil
}

func readJSONSource(path string, useStdin bool, inlineJSON string) (map[string]any, string, error) {
	if inlineJSON != "" {
		payload, err := parseJSONMapBytes([]byte(inlineJSON), "<inline>")
		return payload, "<inline>", err
	}
	if useStdin || path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, "<stdin>", err
		}
		payload, err := parseJSONMapBytes(data, "<stdin>")
		return payload, "<stdin>", err
	}
	if path != "" {
		payload, err := readJSONFile(path)
		return payload, path, err
	}
	return map[string]any{}, "quick", nil
}

func deepCopyMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		out := make(map[string]any, len(value))
		for k, v := range value {
			out[k] = v
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func deepCopyValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}

func parseVarExprs(exprs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, expr := range exprs {
		parts := strings.SplitN(expr, "=", 2)
		if len(parts) != 2 {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Invalid --var expression: %q. Expected KEY=VALUE.", expr),
				ExitCode: 2,
			}
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Invalid --var expression: %q. Variable name cannot be empty.", expr),
				ExitCode: 2,
			}
		}
		out[key] = parts[1]
	}
	return out, nil
}

func substituteVars(payload map[string]any, vars map[string]string) map[string]any {
	if len(vars) == 0 {
		return deepCopyMap(payload)
	}
	out, _ := substituteVarsValue(deepCopyValue(payload), vars).(map[string]any)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func substituteVarsValue(value any, vars map[string]string) any {
	switch v := value.(type) {
	case string:
		return substituteVarsString(v, vars)
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = substituteVarsValue(item, vars)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, substituteVarsValue(item, vars))
		}
		return out
	default:
		return value
	}
}

func substituteVarsString(value string, vars map[string]string) string {
	out := value
	for key, replacement := range vars {
		out = strings.ReplaceAll(out, "${"+key+"}", replacement)
	}
	return out
}

func resolveAssigneeValue(value string) any {
	v := strings.TrimSpace(value)
	lower := strings.ToLower(v)
	if lower == "null" || lower == "none" || v == "" {
		return nil
	}
	if strings.HasPrefix(v, "accountId:") {
		return map[string]any{"accountId": strings.SplitN(v, ":", 2)[1]}
	}
	if strings.HasPrefix(v, "name:") {
		return map[string]any{"name": strings.SplitN(v, ":", 2)[1]}
	}
	if usesBasicJiraAuth() {
		return map[string]any{"name": v}
	}
	return map[string]any{"accountId": v}
}

func resolveInputPath(baseDir string, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	if baseDir == "" || filepath.IsAbs(value) {
		return value, nil
	}
	return safeJoinUnder(baseDir, value)
}

func ensureFieldsMap(payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	fields, _ := payload["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
		payload["fields"] = fields
	}
	return fields
}

func applySetExprsToFields(fields map[string]any, exprs []string) error {
	currentFields := deepCopyMap(fields)
	for _, expr := range exprs {
		field, op, value, err := ParseSetExpr(expr)
		if err != nil {
			return err
		}
		if err := applySetOp(field, op, value, fields, currentFields); err != nil {
			return err
		}
		if updated, ok := fields[field]; ok {
			currentFields[field] = updated
		}
	}
	return nil
}

func resolveCreatePayload(client *Client, input createInput) (createResolution, error) {
	sourceCount := 0
	for _, active := range []bool{
		input.File != "",
		input.UseStdin,
		input.InlineJSON != "",
		input.TemplateFile != "",
		input.CloneIssue != "",
	} {
		if active {
			sourceCount++
		}
	}
	if sourceCount > 1 {
		return createResolution{}, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Use exactly one create source: payload file, --stdin, --inline, --template, or --clone.",
			ExitCode: 2,
		}
	}
	if input.Description != "" && input.DescriptionFile != "" {
		return createResolution{}, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Use either --description or --description-file, not both.",
			ExitCode: 2,
		}
	}

	payload := map[string]any{}
	sourceTarget := map[string]any{"source": "quick"}
	primary := "quick"

	switch {
	case input.TemplateFile != "":
		templatePath, err := resolveInputPath(input.BaseDir, input.TemplateFile)
		if err != nil {
			return createResolution{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsafe template path: %v", err), ExitCode: 2}
		}
		templatePayload, err := readJSONFile(templatePath)
		if err != nil {
			return createResolution{}, err
		}
		vars, err := parseVarExprs(input.TemplateVars)
		if err != nil {
			return createResolution{}, err
		}
		payload = substituteVars(templatePayload, vars)
		sourceTarget = map[string]any{"template": input.TemplateFile, "variables": vars}
		primary = "template"
	case input.CloneIssue != "":
		var err error
		payload, err = buildClonePayload(client, input.CloneIssue, input.CloneMode, input.IncludeFields, input.ExcludeFields)
		if err != nil {
			return createResolution{}, err
		}
		sourceTarget = map[string]any{
			"clone":        ResolveIssueIdentifier(input.CloneIssue),
			"clone_mode":   normalizedCloneMode(input.CloneMode),
			"include":      append([]string(nil), input.IncludeFields...),
			"exclude":      append([]string(nil), input.ExcludeFields...),
			"source_issue": input.CloneIssue,
		}
		primary = "clone"
	default:
		sourcePath := input.File
		if sourcePath != "" {
			resolvedPath, err := resolveInputPath(input.BaseDir, sourcePath)
			if err != nil {
				return createResolution{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsafe create payload path: %v", err), ExitCode: 2}
			}
			sourcePath = resolvedPath
		}
		var err error
		payload, primary, err = readJSONSource(sourcePath, input.UseStdin, input.InlineJSON)
		if err != nil {
			return createResolution{}, err
		}
		switch primary {
		case "<stdin>":
			sourceTarget = map[string]any{"stdin": true}
		case "<inline>":
			sourceTarget = map[string]any{"inline": true}
		case "quick":
			sourceTarget = map[string]any{"source": "quick"}
		default:
			sourceTarget = map[string]any{"file": input.File}
		}
	}

	payload = deepCopyMap(payload)
	fields := ensureFieldsMap(payload)

	if strings.TrimSpace(input.Project) != "" {
		fields["project"] = map[string]any{"key": strings.TrimSpace(input.Project)}
	}
	if strings.TrimSpace(input.IssueType) != "" {
		fields["issuetype"] = map[string]any{"name": strings.TrimSpace(input.IssueType)}
	}
	if strings.TrimSpace(input.Summary) != "" {
		fields["summary"] = strings.TrimSpace(input.Summary)
	}
	if strings.TrimSpace(input.DescriptionFile) != "" {
		descriptionPath, err := resolveInputPath(input.BaseDir, input.DescriptionFile)
		if err != nil {
			return createResolution{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsafe description file path: %v", err), ExitCode: 2}
		}
		content, err := readTextFile(descriptionPath)
		if err != nil {
			return createResolution{}, err
		}
		fields["description"] = content
	}
	if input.Description != "" {
		fields["description"] = input.Description
	}
	if strings.TrimSpace(input.Priority) != "" {
		fields["priority"] = map[string]any{"name": strings.TrimSpace(input.Priority)}
	}
	if strings.TrimSpace(input.Parent) != "" {
		fields["parent"] = map[string]any{"key": ResolveIssueIdentifier(strings.TrimSpace(input.Parent))}
	}
	if strings.TrimSpace(input.Assignee) != "" {
		fields["assignee"] = resolveAssigneeValue(input.Assignee)
	}
	if len(input.Components) > 0 {
		components := make([]map[string]any, 0, len(input.Components))
		for _, component := range input.Components {
			component = strings.TrimSpace(component)
			if component == "" {
				continue
			}
			components = append(components, map[string]any{"name": component})
		}
		fields["components"] = components
	}
	if len(input.Labels) > 0 {
		labels := make([]string, 0, len(input.Labels))
		seen := map[string]bool{}
		for _, label := range input.Labels {
			label = strings.TrimSpace(label)
			if label == "" || seen[label] {
				continue
			}
			labels = append(labels, label)
			seen[label] = true
		}
		fields["labels"] = labels
	}
	if projectKey := safeString(fields, "project", "key"); projectKey == "" {
		if defaultProject := resolveDefaultProjectKey(); defaultProject != "" {
			fields["project"] = map[string]any{"key": defaultProject}
		}
	}
	if err := applySetExprsToFields(fields, input.SetExprs); err != nil {
		return createResolution{}, err
	}

	project := safeString(fields, "project", "key")
	summary := strings.TrimSpace(fmt.Sprintf("%v", fields["summary"]))
	typeName := safeString(fields, "issuetype", "name")
	if project == "" {
		return createResolution{}, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Missing project. Provide --project, include fields.project in the payload, or set jira.default_project/JIRA_PROJECT.",
			ExitCode: 2,
		}
	}
	if summary == "" {
		return createResolution{}, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Missing summary. Provide --summary or include fields.summary in the payload.",
			ExitCode: 2,
		}
	}
	if typeName == "" {
		return createResolution{}, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Missing issue type. Provide --type/--issue-type or include fields.issuetype in the payload.",
			ExitCode: 2,
		}
	}

	return createResolution{
		Payload:      payload,
		SourceTarget: sourceTarget,
		Primary:      primary,
		Summary:      summary,
		Project:      project,
	}, nil
}

func normalizedCloneMode(mode string) string {
	trimmed := strings.ToLower(strings.TrimSpace(mode))
	if trimmed == "" {
		return "portable"
	}
	return trimmed
}

func buildClonePayload(client *Client, issueID string, mode string, includeFields, excludeFields []string) (map[string]any, error) {
	clonedIssue, err := client.GetIssue(ResolveIssueIdentifier(issueID), "*all", "")
	if err != nil {
		return nil, err
	}
	fields, _ := clonedIssue["fields"].(map[string]any)
	if fields == nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  fmt.Sprintf("Issue %s did not return any fields to clone.", ResolveIssueIdentifier(issueID)),
			ExitCode: 1,
		}
	}

	mode = normalizedCloneMode(mode)
	selected := map[string]any{}
	switch mode {
	case "portable":
		names := make([]string, 0, len(portableCloneFields))
		for field := range portableCloneFields {
			names = append(names, field)
		}
		sort.Strings(names)
		for _, field := range names {
			value, ok := fields[field]
			if !ok {
				continue
			}
			if sanitized := sanitizeCloneField(field, value, false); sanitized != nil {
				selected[field] = sanitized
			}
		}
	case "full":
		names := make([]string, 0, len(fields))
		for field := range fields {
			names = append(names, field)
		}
		sort.Strings(names)
		for _, field := range names {
			if cloneDropFields[field] {
				continue
			}
			if sanitized := sanitizeCloneField(field, fields[field], true); sanitized != nil {
				selected[field] = sanitized
			}
		}
	default:
		return nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Unsupported clone mode %q. Use portable or full.", mode),
			ExitCode: 2,
		}
	}

	for _, field := range includeFields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if value, ok := fields[field]; ok {
			if sanitized := sanitizeCloneField(field, value, mode == "full"); sanitized != nil {
				selected[field] = sanitized
			}
		}
	}
	for _, field := range excludeFields {
		delete(selected, strings.TrimSpace(field))
	}

	return map[string]any{"fields": selected}, nil
}

func sanitizeCloneField(field string, value any, fullMode bool) any {
	if cloneDropFields[field] {
		return nil
	}
	switch field {
	case "summary", "description":
		if text := strings.TrimSpace(fmt.Sprintf("%v", value)); text != "" {
			return text
		}
		return nil
	case "project":
		if project, ok := value.(map[string]any); ok {
			if key, ok := project["key"]; ok && fmt.Sprintf("%v", key) != "" {
				return map[string]any{"key": key}
			}
		}
		return nil
	case "issuetype":
		if issueType, ok := value.(map[string]any); ok {
			if name, ok := issueType["name"]; ok && fmt.Sprintf("%v", name) != "" {
				return map[string]any{"name": name}
			}
			if id, ok := issueType["id"]; ok && fmt.Sprintf("%v", id) != "" {
				return map[string]any{"id": id}
			}
		}
		return nil
	case "priority":
		if priority, ok := value.(map[string]any); ok {
			if name, ok := priority["name"]; ok && fmt.Sprintf("%v", name) != "" {
				return map[string]any{"name": name}
			}
			if id, ok := priority["id"]; ok && fmt.Sprintf("%v", id) != "" {
				return map[string]any{"id": id}
			}
		}
		return nil
	case "labels":
		switch labels := value.(type) {
		case []any:
			out := make([]string, 0, len(labels))
			for _, item := range labels {
				s := strings.TrimSpace(fmt.Sprintf("%v", item))
				if s != "" {
					out = append(out, s)
				}
			}
			return out
		case []string:
			out := make([]string, 0, len(labels))
			for _, item := range labels {
				if trimmed := strings.TrimSpace(item); trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		default:
			return nil
		}
	case "components", "fixVersions", "versions":
		items, ok := value.([]any)
		if !ok {
			return nil
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				entry := map[string]any{}
				if id, ok := m["id"]; ok && fmt.Sprintf("%v", id) != "" {
					entry["id"] = id
				} else if name, ok := m["name"]; ok && fmt.Sprintf("%v", name) != "" {
					entry["name"] = name
				}
				if len(entry) > 0 {
					out = append(out, entry)
				}
			}
		}
		return out
	case "assignee":
		if !fullMode {
			return nil
		}
		if assignee, ok := value.(map[string]any); ok {
			if accountID, ok := assignee["accountId"]; ok && fmt.Sprintf("%v", accountID) != "" {
				return map[string]any{"accountId": accountID}
			}
			if name, ok := assignee["name"]; ok && fmt.Sprintf("%v", name) != "" {
				return map[string]any{"name": name}
			}
		}
		return nil
	case "parent":
		if parent, ok := value.(map[string]any); ok {
			if key, ok := parent["key"]; ok && fmt.Sprintf("%v", key) != "" {
				return map[string]any{"key": key}
			}
		}
		return nil
	default:
		if !fullMode {
			return nil
		}
		return sanitizeArbitraryCloneValue(value)
	}
}

func sanitizeArbitraryCloneValue(value any) any {
	switch v := value.(type) {
	case nil, string, bool, float64, int, int64:
		return v
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			if sanitized := sanitizeArbitraryCloneValue(item); sanitized != nil {
				out = append(out, sanitized)
			}
		}
		return out
	case map[string]any:
		if accountID, ok := v["accountId"]; ok && fmt.Sprintf("%v", accountID) != "" {
			return map[string]any{"accountId": accountID}
		}
		if key, ok := v["key"]; ok && fmt.Sprintf("%v", key) != "" {
			return map[string]any{"key": key}
		}
		if valueField, ok := v["value"]; ok && fmt.Sprintf("%v", valueField) != "" {
			out := map[string]any{"value": sanitizeArbitraryCloneValue(valueField)}
			if child, ok := v["child"]; ok {
				if sanitizedChild := sanitizeArbitraryCloneValue(child); sanitizedChild != nil {
					out["child"] = sanitizedChild
				}
			}
			if id, ok := v["id"]; ok && fmt.Sprintf("%v", id) != "" {
				out["id"] = id
			}
			return out
		}
		if name, ok := v["name"]; ok && fmt.Sprintf("%v", name) != "" {
			out := map[string]any{"name": name}
			if id, ok := v["id"]; ok && fmt.Sprintf("%v", id) != "" {
				out["id"] = id
			}
			return out
		}
		if id, ok := v["id"]; ok && fmt.Sprintf("%v", id) != "" {
			return map[string]any{"id": id}
		}
		out := map[string]any{}
		for key, item := range v {
			switch key {
			case "self", "avatarUrls", "iconUrl", "displayName", "emailAddress", "active", "timeZone", "projectTypeKey":
				continue
			}
			if sanitized := sanitizeArbitraryCloneValue(item); sanitized != nil {
				out[key] = sanitized
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return fmt.Sprintf("%v", value)
	}
}
