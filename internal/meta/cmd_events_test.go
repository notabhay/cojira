package meta

import (
	"bytes"
	"testing"

	"github.com/notabhay/cojira/internal/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventsTailLatest(t *testing.T) {
	t.Setenv("COJIRA_EVENT_DIR", t.TempDir())
	_, err := events.Append("stream-1", map[string]any{"type": "progress", "message": "hello"})
	require.NoError(t, err)

	cmd := NewEventsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"tail", "--latest"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), `"hello"`)
}
