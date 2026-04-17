package jira

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBulkUpdateResumesCompletedItems(t *testing.T) {
	idemDir := t.TempDir()
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", idemDir)

	putCounts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_, _ = w.Write([]byte(`{"total":2,"issues":[{"key":"PROJ-1"},{"key":"PROJ-2"}]}`))
		case "/rest/api/2/issue/PROJ-1":
			putCounts["PROJ-1"]++
			w.WriteHeader(204)
		case "/rest/api/2/issue/PROJ-2":
			putCounts["PROJ-2"]++
			if putCounts["PROJ-2"] == 1 {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			w.WriteHeader(204)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "fake-token")
	t.Setenv("JIRA_AUTH_MODE", "bearer")

	payloadPath := filepath.Join(t.TempDir(), "payload.json")
	require.NoError(t, os.WriteFile(payloadPath, []byte(`{"fields":{"summary":"Retry-safe summary"}}`), 0o644))

	run := func() error {
		cmd := NewBulkUpdateCmd()
		require.NoError(t, cmd.Flags().Set("jql", "project = PROJ"))
		require.NoError(t, cmd.Flags().Set("payload", payloadPath))
		require.NoError(t, cmd.Flags().Set("idempotency-key", "resume-bulk-update"))
		require.NoError(t, cmd.Flags().Set("quiet", "true"))
		return runBulkUpdate(cmd, nil)
	}

	err := run()
	require.Error(t, err)
	assert.Equal(t, 1, putCounts["PROJ-1"])
	assert.Equal(t, 1, putCounts["PROJ-2"])

	err = run()
	require.NoError(t, err)
	assert.Equal(t, 1, putCounts["PROJ-1"])
	assert.Equal(t, 2, putCounts["PROJ-2"])

	err = run()
	require.NoError(t, err)
	assert.Equal(t, 1, putCounts["PROJ-1"])
	assert.Equal(t, 2, putCounts["PROJ-2"])
}

func TestRunBatchResumesCompletedOperations(t *testing.T) {
	idemDir := t.TempDir()
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", idemDir)

	putCounts := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/PROJ-1":
			putCounts["PROJ-1"]++
			w.WriteHeader(204)
		case "/rest/api/2/issue/PROJ-2":
			putCounts["PROJ-2"]++
			if putCounts["PROJ-2"] == 1 {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			w.WriteHeader(204)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "fake-token")
	t.Setenv("JIRA_AUTH_MODE", "bearer")

	workDir := t.TempDir()
	payload1 := filepath.Join(workDir, "p1.json")
	payload2 := filepath.Join(workDir, "p2.json")
	require.NoError(t, os.WriteFile(payload1, []byte(`{"fields":{"summary":"one"}}`), 0o644))
	require.NoError(t, os.WriteFile(payload2, []byte(`{"fields":{"summary":"two"}}`), 0o644))

	configPath := filepath.Join(workDir, "batch.json")
	config := fmt.Sprintf(`{
  "operations": [
    {"op":"update","issue":"PROJ-1","file":"%s"},
    {"op":"update","issue":"PROJ-2","file":"%s"}
  ]
}`, filepath.Base(payload1), filepath.Base(payload2))
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o644))

	run := func() error {
		cmd := NewBatchCmd()
		require.NoError(t, cmd.Flags().Set("idempotency-key", "resume-batch"))
		require.NoError(t, cmd.Flags().Set("quiet", "true"))
		return runBatch(cmd, []string{configPath})
	}

	err := run()
	require.Error(t, err)
	assert.Equal(t, 1, putCounts["PROJ-1"])
	assert.Equal(t, 1, putCounts["PROJ-2"])

	err = run()
	require.NoError(t, err)
	assert.Equal(t, 1, putCounts["PROJ-1"])
	assert.Equal(t, 2, putCounts["PROJ-2"])

	err = run()
	require.NoError(t, err)
	assert.Equal(t, 1, putCounts["PROJ-1"])
	assert.Equal(t, 2, putCounts["PROJ-2"])
}
