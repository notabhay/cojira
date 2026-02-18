package argv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- ConsumeLeadingFlags tests ---

func TestConsumeLeadingFlagsBasic(t *testing.T) {
	argv := []string{"--debug", "--timeout", "60", "jira", "info"}
	collected, rest := ConsumeLeadingFlags(argv, NetworkFlagArity, nil)
	assert.Equal(t, []string{"--debug", "--timeout", "60"}, collected)
	assert.Equal(t, []string{"jira", "info"}, rest)
}

func TestConsumeLeadingFlagsEmpty(t *testing.T) {
	collected, rest := ConsumeLeadingFlags(nil, NetworkFlagArity, nil)
	assert.Nil(t, collected)
	assert.Nil(t, rest)
}

func TestConsumeLeadingFlagsStopToken(t *testing.T) {
	argv := []string{"--debug", "jira", "--timeout", "60"}
	collected, rest := ConsumeLeadingFlags(argv, NetworkFlagArity, []string{"jira", "confluence"})
	assert.Equal(t, []string{"--debug"}, collected)
	assert.Equal(t, []string{"jira", "--timeout", "60"}, rest)
}

func TestConsumeLeadingFlagsDoubleDash(t *testing.T) {
	argv := []string{"--debug", "--", "--timeout", "60"}
	collected, rest := ConsumeLeadingFlags(argv, NetworkFlagArity, nil)
	assert.Equal(t, []string{"--debug"}, collected)
	assert.Equal(t, []string{"--", "--timeout", "60"}, rest)
}

func TestConsumeLeadingFlagsEqualsForm(t *testing.T) {
	argv := []string{"--timeout=30", "jira"}
	collected, rest := ConsumeLeadingFlags(argv, NetworkFlagArity, nil)
	assert.Equal(t, []string{"--timeout=30"}, collected)
	assert.Equal(t, []string{"jira"}, rest)
}

func TestConsumeLeadingFlagsUnknownFirst(t *testing.T) {
	argv := []string{"jira", "--debug"}
	collected, rest := ConsumeLeadingFlags(argv, NetworkFlagArity, nil)
	assert.Nil(t, collected)
	assert.Equal(t, []string{"jira", "--debug"}, rest)
}

// --- ReorderKnownFlags tests ---

func TestReorderKnownFlagsMovesToFront(t *testing.T) {
	argv := []string{"whoami", "--output-mode", "summary", "--debug"}
	result := ReorderKnownFlags(argv, NetworkFlagArity)
	assert.Equal(t, []string{"--debug", "whoami", "--output-mode", "summary"}, result)
}

func TestReorderKnownFlagsWithValue(t *testing.T) {
	argv := []string{"info", "PROJ-123", "--timeout", "60", "--debug"}
	result := ReorderKnownFlags(argv, NetworkFlagArity)
	assert.Equal(t, []string{"--timeout", "60", "--debug", "info", "PROJ-123"}, result)
}

func TestReorderKnownFlagsDoubleDash(t *testing.T) {
	argv := []string{"cmd", "--", "--debug"}
	result := ReorderKnownFlags(argv, NetworkFlagArity)
	assert.Equal(t, []string{"cmd", "--", "--debug"}, result)
}

func TestReorderKnownFlagsEqualsForm(t *testing.T) {
	argv := []string{"cmd", "--timeout=30", "--debug"}
	result := ReorderKnownFlags(argv, NetworkFlagArity)
	assert.Equal(t, []string{"--timeout=30", "--debug", "cmd"}, result)
}

func TestReorderKnownFlagsNoFlags(t *testing.T) {
	argv := []string{"jira", "info", "PROJ-123"}
	result := ReorderKnownFlags(argv, NetworkFlagArity)
	assert.Equal(t, []string{"jira", "info", "PROJ-123"}, result)
}

func TestReorderKnownFlagsEmpty(t *testing.T) {
	result := ReorderKnownFlags(nil, NetworkFlagArity)
	assert.Nil(t, result)
}

func TestReorderKnownFlagsAllFlags(t *testing.T) {
	argv := []string{"--debug", "--timeout", "30"}
	result := ReorderKnownFlags(argv, NetworkFlagArity)
	assert.Equal(t, []string{"--debug", "--timeout", "30"}, result)
}

func TestReorderKnownFlagsPreservesOrder(t *testing.T) {
	argv := []string{"a", "--debug", "b", "--timeout", "60", "c"}
	result := ReorderKnownFlags(argv, NetworkFlagArity)
	assert.Equal(t, []string{"--debug", "--timeout", "60", "a", "b", "c"}, result)
}
