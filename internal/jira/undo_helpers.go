package jira

import (
	"encoding/json"

	"github.com/notabhay/cojira/internal/undo"
)

func snapshotFieldValues(currentFields map[string]any, fieldNames []string) map[string]any {
	if len(fieldNames) == 0 {
		return nil
	}
	out := make(map[string]any, len(fieldNames))
	for _, field := range fieldNames {
		if value, ok := currentFields[field]; ok {
			out[field] = cloneUndoValue(value)
			continue
		}
		out[field] = nil
	}
	return out
}

func payloadFieldNames(payload map[string]any) []string {
	fields, _ := payload["fields"].(map[string]any)
	names := make([]string, 0, len(fields))
	for field := range fields {
		names = append(names, field)
	}
	return names
}

func recordUndoEntry(groupID, issueID, operation string, fields map[string]any, fromStatus, toStatus string) {
	if groupID == "" {
		groupID = undo.NewGroupID(operation)
	}
	_ = undo.RecordIssue(undo.IssueEntry{
		GroupID:    groupID,
		Issue:      issueID,
		Operation:  operation,
		Fields:     fields,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
	})
}

func recordUndoAction(groupID, issueID, operation, action string, payload map[string]any) {
	if groupID == "" {
		groupID = undo.NewGroupID(operation)
	}
	_ = undo.RecordIssue(undo.IssueEntry{
		GroupID:    groupID,
		Issue:      issueID,
		Operation:  operation,
		UndoAction: action,
		Payload:    cloneAnyMap(payload),
	})
}

func cloneUndoValue(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return value
	}
	return out
}
