package logging

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLoggerJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(Config{
		Enabled:   true,
		Component: "jira",
		Format:    "json",
		Writer:    &buf,
	})
	require.NotNil(t, logger)

	logger.Debug("http.request", "method", "GET", "target", "jira.example.com/rest/api/2/issue/PROJ-1")

	output := buf.String()
	assert.Contains(t, output, `"msg":"http.request"`)
	assert.Contains(t, output, `"component":"jira"`)
	assert.Contains(t, output, `"method":"GET"`)
}

func TestSafeTarget(t *testing.T) {
	assert.Equal(t, "jira.example.com/rest/api/2/issue/PROJ-1?expand=changelog", SafeTarget("https://jira.example.com/rest/api/2/issue/PROJ-1?expand=changelog"))
	assert.Equal(t, "/rest/api/2/issue/PROJ-1", SafeTarget("/rest/api/2/issue/PROJ-1"))
}
