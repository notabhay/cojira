package board

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBoardJiraCmd() *cobra.Command {
	cmd := jira.NewJiraCmd()
	RegisterBoardCommands(cmd, jira.ClientFromCmd)
	return cmd
}

func executeBoardJSONCommand(t *testing.T, cmd *cobra.Command, args []string, out *map[string]any) error {
	t.Helper()
	output.SetMode("")

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	cmd.SetArgs(args)
	err = cmd.Execute()

	_ = w.Close()
	buf, _ := io.ReadAll(r)
	require.NotEmpty(t, buf)
	require.NoError(t, json.Unmarshal(buf, out))
	return err
}

func TestBoardSwimlanesMoveRequiresFirstOrAfter(t *testing.T) {
	cmd := newTestBoardJiraCmd()
	cmd.SetArgs([]string{"--experimental", "board-swimlanes", "move", "45434", "12"})

	err := cmd.Execute()
	require.Error(t, err)

	var ce *cerrors.CojiraError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, 2, ce.ExitCode)
	assert.Contains(t, ce.Message, "Provide either --first")
}

func TestBoardSwimlanesMoveRejectsFirstAndAfterTogether(t *testing.T) {
	cmd := newTestBoardJiraCmd()
	cmd.SetArgs([]string{"--experimental", "board-swimlanes", "move", "45434", "12", "--first", "--after", "7"})

	err := cmd.Execute()
	require.Error(t, err)

	var ce *cerrors.CojiraError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, 2, ce.ExitCode)
	assert.Contains(t, ce.Message, "mutually exclusive")
}

func TestBoardSwimlanesSimulateJSONAllDefaultWhenNonDefaultQueriesEmpty(t *testing.T) {
	var searchCalled atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/greenhopper/1.0/rapidviewconfig/editmodel":
			assert.Equal(t, "45434", r.URL.Query().Get("rapidViewId"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"swimlanesConfig": map[string]any{
					"swimlaneStrategy": "custom",
					"canEdit":          true,
					"swimlanes": []any{
						map[string]any{"id": 1.0, "name": "Inbox", "query": "", "description": "", "isDefault": false},
						map[string]any{"id": 2.0, "name": "Default", "query": "", "description": "", "isDefault": true},
					},
				},
			})
		case "/rest/agile/1.0/board/45434/issue":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total": 2,
				"issues": []any{
					map[string]any{"key": "PROJ-1"},
					map[string]any{"key": "PROJ-2"},
				},
			})
		case "/rest/api/2/search":
			searchCalled.Store(true)
			_ = json.NewEncoder(w).Encode(map[string]any{"total": 0, "issues": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeBoardJSONCommand(t, newTestBoardJiraCmd(), []string{
		"--experimental", "board-swimlanes", "simulate", "45434", "--output-mode", "json",
	}, &payload))

	assert.False(t, searchCalled.Load())
	assert.Equal(t, true, payload["ok"])

	result := payload["result"].(map[string]any)
	routingSummary := result["routing_summary"].(map[string]any)
	assert.Equal(t, true, routingSummary["all_default"])
	assert.Contains(t, routingSummary["warning"], "empty JQL")

	summary := result["summary"].(map[string]any)
	assert.Equal(t, float64(2), summary["default_assigned"])
	assert.Equal(t, float64(2), summary["no_match"])

	noMatch := result["noMatch"].([]any)
	assert.Len(t, noMatch, 2)

	issues := result["issues"].([]any)
	require.Len(t, issues, 2)
	firstIssue := issues[0].(map[string]any)
	assert.Equal(t, "Default", firstIssue["assignedSwimlane"])
	assert.Equal(t, false, firstIssue["ambiguous"])
}
