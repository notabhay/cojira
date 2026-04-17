package jira

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/notabhay/cojira/internal/undo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommentAddRecordsUndoDelete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_UNDO_DIR", dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-1/comment", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "9001"})
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewCommentCmd()
	cmd.SetArgs([]string{"PROJ-1", "--add", "hello", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)
	assert.Equal(t, true, payload["ok"])

	entries, err := undo.ListIssues(1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "comment.delete", entries[0].UndoAction)
	assert.Equal(t, "9001", entries[0].Payload["comment_id"])
}

func TestUndoApplyCommentDelete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_UNDO_DIR", dir)
	require.NoError(t, undo.RecordIssue(undo.IssueEntry{
		ID:         "entry-1",
		GroupID:    "group-1",
		Issue:      "PROJ-1",
		Operation:  "jira.comment.add",
		UndoAction: "comment.delete",
		Payload:    map[string]any{"comment_id": "77"},
		Timestamp:  time.Now().UTC(),
	}))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-1/comment/77", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewUndoCmd()
	cmd.SetArgs([]string{"apply", "PROJ-1", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)
	assert.Equal(t, true, payload["ok"])
}

func TestUndoApplyWorklogUpdate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_UNDO_DIR", dir)
	require.NoError(t, undo.RecordIssue(undo.IssueEntry{
		ID:         "entry-2",
		GroupID:    "group-2",
		Issue:      "PROJ-1",
		Operation:  "jira.worklog.update",
		UndoAction: "worklog.update",
		Payload: map[string]any{
			"worklog_id": "55",
			"payload": map[string]any{
				"timeSpent": "1h",
				"started":   "2026-04-17T10:00:00.000+0900",
				"comment":   "before",
			},
		},
		Timestamp: time.Now().UTC(),
	}))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-1/worklog/55", r.URL.Path)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "1h", payload["timeSpent"])
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "55"})
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewUndoCmd()
	cmd.SetArgs([]string{"apply", "PROJ-1", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)
	assert.Equal(t, true, payload["ok"])
}

func TestUndoApplyAttachmentDelete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_UNDO_DIR", dir)
	require.NoError(t, undo.RecordIssue(undo.IssueEntry{
		ID:         "entry-3",
		GroupID:    "group-3",
		Issue:      "PROJ-1",
		Operation:  "jira.attachment.upload",
		UndoAction: "attachment.delete",
		Payload:    map[string]any{"attachment_id": "99"},
		Timestamp:  time.Now().UTC(),
	}))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/attachment/99", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewUndoCmd()
	cmd.SetArgs([]string{"apply", "PROJ-1", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)
	assert.Equal(t, true, payload["ok"])
}

func TestAttachmentUploadRecordsUndoDelete(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_UNDO_DIR", dir)

	uploadDir := t.TempDir()
	filePath := filepath.Join(uploadDir, "sample.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-1/attachments", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "55", "filename": "sample.txt"}})
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewAttachmentCmd()
	cmd.SetArgs([]string{"PROJ-1", "--upload", filePath, "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)
	assert.Equal(t, true, payload["ok"])

	entries, err := undo.ListIssues(1)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "attachment.delete", entries[0].UndoAction)
	assert.Equal(t, "55", entries[0].Payload["attachment_id"])
}
