package main

import (
	"io"
	"os"
	"testing"

	"github.com/notabhay/cojira/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRootCmdBoardCommandsReturnErrorsInsteadOfPanicking(t *testing.T) {
	output.SetMode("")
	t.Setenv("JIRA_BASE_URL", "")
	t.Setenv("JIRA_API_TOKEN", "")

	root := buildRootCmd()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	root.SetArgs([]string{"jira", "--experimental", "board-swimlanes", "get", "123", "--output-mode", "json"})
	err = root.Execute()

	_ = w.Close()
	_, _ = io.ReadAll(r)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JIRA_BASE_URL")
}
