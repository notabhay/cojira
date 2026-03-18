package jira

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
)

func batchPlanStoreKey(idemKey string) string {
	return idemKey + ".plan"
}

func batchCaptureStoreKey(idemKey, opID, variable string) string {
	return fmt.Sprintf("%s.capture.%s.%s", idemKey, opID, variable)
}

func resolveJiraBatchPlan(client *Client, useStdin bool, configFile string, dryRun bool, freezePlan bool, requestedKey string) (jiraBatchPlan, string, map[string]any, error) {
	if requestedKey != "" {
		var stored jiraBatchPlan
		found, err := idempotency.LoadValue(batchPlanStoreKey(requestedKey), &stored)
		if err != nil {
			return jiraBatchPlan{}, "", nil, err
		}
		if found {
			return stored, requestedKey, targetForBatchSource(stored.Source), nil
		}
	}

	operations, basePath, source, err := loadBatchOperations(useStdin, configFile)
	if err != nil {
		return jiraBatchPlan{}, "", nil, err
	}

	planOps := make([]jiraBatchPlanOp, 0, len(operations))
	for idx, op := range operations {
		planOp, err := resolveJiraBatchPlanOp(client, op, basePath, idx)
		if err != nil {
			return jiraBatchPlan{}, "", nil, err
		}
		planOps = append(planOps, planOp)
	}

	plan := jiraBatchPlan{
		Version:    2,
		Source:     source,
		Operations: planOps,
	}

	idemKey := requestedKey
	if idemKey == "" {
		idemKey = output.IdempotencyKey("jira.batch", plan)
	}

	var stored jiraBatchPlan
	found, err := idempotency.LoadValue(batchPlanStoreKey(idemKey), &stored)
	if err != nil {
		return jiraBatchPlan{}, "", nil, err
	}
	if found {
		return stored, idemKey, targetForBatchSource(stored.Source), nil
	}
	if !dryRun || freezePlan {
		if err := idempotency.RecordKindValue(batchPlanStoreKey(idemKey), "plan", "jira.batch plan", plan); err != nil {
			return jiraBatchPlan{}, "", nil, err
		}
	}
	return plan, idemKey, targetForBatchSource(source), nil
}

func loadBatchOperations(useStdin bool, configFile string) ([]map[string]any, string, string, error) {
	if useStdin && configFile != "" {
		return nil, "", "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Cannot use both --stdin and a config file.", ExitCode: 2}
	}

	if useStdin {
		data, err := ioReadAllOrScan()
		if err != nil {
			return nil, "", "", err
		}
		basePath, _ := os.Getwd()
		operations, resolvedBase, err := parseBatchData(data, basePath)
		if err != nil {
			return nil, "", "", err
		}
		if resolvedBase != "" {
			basePath = resolvedBase
		}
		return operations, basePath, "stdin", nil
	}

	if configFile == "" {
		return nil, "", "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Provide a config file, --stdin, or --idempotency-key for a saved resumable run.", ExitCode: 2}
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, "", "", &cerrors.CojiraError{Code: cerrors.FileNotFound, Message: fmt.Sprintf("Config file not found: %s", configFile), ExitCode: 1}
	}
	basePath := filepath.Dir(configFile)
	operations, resolvedBase, err := parseBatchData(data, basePath)
	if err != nil {
		return nil, "", "", err
	}
	if resolvedBase != "" {
		basePath = resolvedBase
	}
	return operations, basePath, configFile, nil
}

func ioReadAllOrScan() ([]byte, error) {
	data, err := io.ReadAll(os.Stdin)
	if err == nil && len(data) > 0 {
		return data, nil
	}
	return data, err
}

func parseBatchData(data []byte, defaultBase string) ([]map[string]any, string, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, "", &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: "Batch config is empty.", ExitCode: 1}
	}

	if strings.HasPrefix(trimmed, "{") {
		var config map[string]any
		if err := json.Unmarshal([]byte(trimmed), &config); err != nil {
			return nil, "", &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: fmt.Sprintf("Invalid JSON batch config: %v", err), ExitCode: 1}
		}
		operations := mapSlice(config["operations"])
		basePath := defaultBase
		if bd := strings.TrimSpace(fmt.Sprintf("%v", config["base_dir"])); bd != "" && bd != "<nil>" {
			resolved, err := resolveInputPath(defaultBase, bd)
			if err != nil {
				return nil, "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsafe batch base_dir: %v", err), ExitCode: 2}
			}
			basePath = resolved
		}
		return operations, basePath, nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var ops []map[string]any
		if err := json.Unmarshal([]byte(trimmed), &ops); err != nil {
			return nil, "", &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: fmt.Sprintf("Invalid JSON batch operations: %v", err), ExitCode: 1}
		}
		return ops, defaultBase, nil
	}

	var operations []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(trimmed))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var op map[string]any
		if err := json.Unmarshal([]byte(line), &op); err != nil {
			return nil, "", &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: fmt.Sprintf("Invalid JSON operation on stdin: %v", err), ExitCode: 1}
		}
		operations = append(operations, op)
	}
	if scanner.Err() != nil {
		return nil, "", scanner.Err()
	}
	return operations, defaultBase, nil
}

func resolveJiraBatchPlanOp(client *Client, op map[string]any, basePath string, idx int) (jiraBatchPlanOp, error) {
	opType := strings.ToLower(strings.TrimSpace(batchString(op, "op")))
	switch opType {
	case "create":
		return resolveBatchCreatePlanOp(client, op, basePath, idx)
	case "update":
		return resolveBatchUpdatePlanOp(client, op, basePath, idx)
	case "transition":
		return resolveBatchTransitionPlanOp(client, op, basePath, idx)
	default:
		return jiraBatchPlanOp{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unknown operation: %s", opType), ExitCode: 1}
	}
}

func resolveBatchCreatePlanOp(client *Client, op map[string]any, basePath string, idx int) (jiraBatchPlanOp, error) {
	typeName := firstNonEmpty(batchString(op, "type"), batchString(op, "issue_type"), batchString(op, "issue-type"))
	templateVars := batchStringSlice(op, "var")
	if len(templateVars) == 0 {
		templateVars = batchStringSlice(op, "vars")
	}
	resolution, err := resolveCreatePayload(client, createInput{
		File:            batchString(op, "file"),
		InlineJSON:      batchInlineJSON(op),
		TemplateFile:    batchString(op, "template"),
		TemplateVars:    templateVars,
		CloneIssue:      batchString(op, "clone"),
		CloneMode:       batchString(op, "clone_mode"),
		IncludeFields:   batchStringSlice(op, "include_field"),
		ExcludeFields:   batchStringSlice(op, "exclude_field"),
		BaseDir:         basePath,
		Project:         batchString(op, "project"),
		IssueType:       typeName,
		Summary:         batchString(op, "summary"),
		Description:     batchString(op, "description"),
		DescriptionFile: batchString(op, "description_file"),
		Priority:        batchString(op, "priority"),
		Parent:          batchString(op, "parent"),
		Assignee:        batchString(op, "assignee"),
		Components:      batchStringSliceAny(op, "component", "components"),
		Labels:          batchStringSliceAny(op, "label", "labels"),
		SetExprs:        batchStringSlice(op, "set"),
	})
	if err != nil {
		return jiraBatchPlanOp{}, err
	}
	capture := batchString(op, "capture")
	description := strings.TrimSpace(batchStringAny(op, "op_description", "operation_description"))
	if description == "" {
		description = fmt.Sprintf("create issue: %s", resolution.Summary)
	}
	target := deepCopyMap(resolution.SourceTarget)
	target["project"] = resolution.Project
	target["summary"] = resolution.Summary
	if capture != "" {
		target["capture"] = capture
	}
	return jiraBatchPlanOp{
		ID:          fmt.Sprintf("%04d-%s", idx, output.IdempotencyKey("create", resolution.Payload, capture)[:12]),
		Op:          "create",
		Description: description,
		Target:      target,
		Payload:     resolution.Payload,
		Notify:      batchNotifyValue(op, true),
		Capture:     capture,
	}, nil
}

func resolveBatchUpdatePlanOp(client *Client, op map[string]any, basePath string, idx int) (jiraBatchPlanOp, error) {
	issueID := strings.TrimSpace(batchString(op, "issue"))
	if issueID == "" {
		return jiraBatchPlanOp{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch update operations require an issue field.", ExitCode: 2}
	}
	payload, err := resolveBatchUpdatePayload(client, issueID, op, basePath)
	if err != nil {
		return jiraBatchPlanOp{}, err
	}
	description := strings.TrimSpace(batchStringAny(op, "op_description", "operation_description"))
	if description == "" {
		description = fmt.Sprintf("update %s", issueID)
	}
	return jiraBatchPlanOp{
		ID:          fmt.Sprintf("%04d-%s", idx, output.IdempotencyKey("update", issueID, payload)[:12]),
		Op:          "update",
		Description: description,
		Target:      map[string]any{"issue": issueID},
		Payload:     payload,
		Notify:      batchNotifyValue(op, true),
	}, nil
}

func resolveBatchTransitionPlanOp(client *Client, op map[string]any, basePath string, idx int) (jiraBatchPlanOp, error) {
	issueID := strings.TrimSpace(batchString(op, "issue"))
	if issueID == "" {
		return jiraBatchPlanOp{}, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch transition operations require an issue field.", ExitCode: 2}
	}
	payload, transitionID, description, err := resolveBatchTransitionPayload(client, issueID, op, basePath)
	if err != nil {
		return jiraBatchPlanOp{}, err
	}
	return jiraBatchPlanOp{
		ID:          fmt.Sprintf("%04d-%s", idx, output.IdempotencyKey("transition", issueID, payload)[:12]),
		Op:          "transition",
		Description: description,
		Target:      map[string]any{"issue": issueID, "transition": transitionID},
		Payload:     payload,
		Notify:      batchNotifyValue(op, true),
	}, nil
}

func resolveBatchUpdatePayload(client *Client, issueExpr string, op map[string]any, basePath string) (map[string]any, error) {
	payload, err := resolveBatchPayloadSource(op, basePath)
	if err != nil {
		return nil, err
	}
	fields := ensureFieldsMap(payload)

	if summary := batchString(op, "summary"); strings.TrimSpace(summary) != "" {
		fields["summary"] = strings.TrimSpace(summary)
	}
	descriptionFile := batchString(op, "description_file")
	if descriptionFile != "" && batchString(op, "description") != "" {
		return nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch update uses either description or description_file, not both.", ExitCode: 2}
	}
	if descriptionFile != "" {
		path, err := resolveInputPath(basePath, descriptionFile)
		if err != nil {
			return nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsafe description file path: %v", err), ExitCode: 2}
		}
		content, err := readTextFile(path)
		if err != nil {
			return nil, err
		}
		fields["description"] = content
	}
	if description := batchString(op, "description"); strings.TrimSpace(description) != "" {
		fields["description"] = description
	}
	if due := batchString(op, "due"); strings.TrimSpace(due) != "" {
		fields["duedate"] = strings.TrimSpace(due)
	}

	setExprs := batchStringSlice(op, "set")
	componentFlags := batchStringSliceAny(op, "component", "components")
	issueID := ResolveIssueIdentifier(issueExpr)
	hasIssuePlaceholders := strings.Contains(issueExpr, "${")

	refFields := map[string]bool{}
	for k := range fields {
		refFields[k] = true
	}
	for _, expr := range setExprs {
		field, _, _, err := ParseSetExpr(expr)
		if err != nil {
			return nil, err
		}
		refFields[field] = true
	}
	if len(componentFlags) > 0 {
		refFields["project"] = true
		refFields["components"] = true
	}

	currentFields := map[string]any{}
	if !hasIssuePlaceholders && (len(setExprs) > 0 || len(componentFlags) > 0) {
		fieldNames := make([]string, 0, len(refFields))
		for key := range refFields {
			fieldNames = append(fieldNames, key)
		}
		sort.Strings(fieldNames)
		issue, err := client.GetIssue(issueID, joinComma(fieldNames), "")
		if err != nil {
			return nil, err
		}
		currentFields, _ = issue["fields"].(map[string]any)
		if currentFields == nil {
			currentFields = map[string]any{}
		}
	}
	if hasIssuePlaceholders && (len(setExprs) > 0 || len(componentFlags) > 0) {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Batch update for %q cannot use set/component overlays until the issue placeholder is resolved at runtime.", issueExpr),
			ExitCode: 2,
		}
	}

	currentContext := deepCopyMap(currentFields)
	for key, value := range fields {
		currentContext[key] = value
	}
	for _, expr := range setExprs {
		field, op, value, err := ParseSetExpr(expr)
		if err != nil {
			return nil, err
		}
		if err := applySetOp(field, op, value, fields, currentContext); err != nil {
			return nil, err
		}
		if updated, ok := fields[field]; ok {
			currentContext[field] = updated
		}
	}
	if len(componentFlags) > 0 {
		projectKey := safeString(currentFields, "project", "key")
		if projectKey == "" {
			return nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Could not determine the issue's project for component lookup.", ExitCode: 1}
		}
		resolved, err := resolveProjectComponentsByName(client, projectKey, componentFlags)
		if err != nil {
			return nil, err
		}
		fields["components"] = resolved
	}

	payload["fields"] = fields
	return payload, nil
}

func resolveBatchTransitionPayload(client *Client, issueExpr string, op map[string]any, basePath string) (map[string]any, string, string, error) {
	payload, err := resolveBatchPayloadSource(op, basePath)
	if err != nil {
		return nil, "", "", err
	}
	issueID := ResolveIssueIdentifier(issueExpr)
	transitionID := strings.TrimSpace(batchStringAny(op, "transition", "transition_id"))
	toStatus := strings.TrimSpace(batchString(op, "to"))
	if transitionID == "" && toStatus == "" {
		return nil, "", "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch transition operations require either transition or to.", ExitCode: 2}
	}
	if transitionID == "" {
		if strings.Contains(issueExpr, "${") {
			return nil, "", "", &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Batch transition for %q requires an explicit transition id when the issue identifier is dynamic.", issueExpr),
				ExitCode: 2,
			}
		}
		data, err := client.ListTransitions(issueID)
		if err != nil {
			return nil, "", "", err
		}
		transitions, _ := data["transitions"].([]any)
		matches := filterTransitionsByStatus(transitions, toStatus)
		if len(matches) == 0 {
			return nil, "", "", &cerrors.CojiraError{Code: cerrors.TransitionNotFound, Message: fmt.Sprintf("No transition to status %q found for %s.", toStatus, issueID), ExitCode: 1}
		}
		first, _ := matches[0].(map[string]any)
		transitionID = fmt.Sprintf("%v", first["id"])
	}
	payload["transition"] = map[string]any{"id": transitionID}
	description := strings.TrimSpace(batchStringAny(op, "op_description", "operation_description"))
	if description == "" {
		description = fmt.Sprintf("transition %s using %s", issueExpr, firstNonEmpty(toStatus, transitionID))
	}
	return payload, transitionID, description, nil
}

func resolveBatchPayloadSource(op map[string]any, basePath string) (map[string]any, error) {
	switch {
	case batchString(op, "template") != "":
		templatePath, err := resolveInputPath(basePath, batchString(op, "template"))
		if err != nil {
			return nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsafe template path: %v", err), ExitCode: 2}
		}
		templatePayload, err := readJSONFile(templatePath)
		if err != nil {
			return nil, err
		}
		templateVars := batchStringSlice(op, "var")
		if len(templateVars) == 0 {
			templateVars = batchStringSlice(op, "vars")
		}
		vars, err := parseVarExprs(templateVars)
		if err != nil {
			return nil, err
		}
		return substituteVars(templatePayload, vars), nil
	case batchInlineJSON(op) != "":
		payload, _, err := readJSONSource("", false, batchInlineJSON(op))
		return payload, err
	case batchString(op, "file") != "":
		path, err := resolveInputPath(basePath, batchString(op, "file"))
		if err != nil {
			return nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsafe payload path: %v", err), ExitCode: 2}
		}
		return readJSONFile(path)
	default:
		return map[string]any{}, nil
	}
}

func batchString(op map[string]any, key string) string {
	value, _ := op[key]
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func batchStringAny(op map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := batchString(op, key); value != "" {
			return value
		}
	}
	return ""
}

func batchStringSlice(op map[string]any, key string) []string {
	value, ok := op[key]
	if !ok || value == nil {
		return nil
	}
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(fmt.Sprintf("%v", item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{strings.TrimSpace(v)}
	default:
		return nil
	}
}

func batchStringSliceAny(op map[string]any, keys ...string) []string {
	for _, key := range keys {
		if values := batchStringSlice(op, key); len(values) > 0 {
			return values
		}
	}
	return nil
}

func batchInlineJSON(op map[string]any) string {
	for _, key := range []string{"inline", "payload"} {
		if value, ok := op[key]; ok && value != nil {
			switch v := value.(type) {
			case string:
				return strings.TrimSpace(v)
			case map[string]any:
				data, _ := json.Marshal(v)
				return string(data)
			}
		}
	}
	return ""
}

func batchNotifyValue(op map[string]any, fallback bool) bool {
	if value, ok := op["notify"].(bool); ok {
		return value
	}
	if value, ok := op["no_notify"].(bool); ok {
		return !value
	}
	return fallback
}

func mapSlice(value any) []map[string]any {
	items, _ := value.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
