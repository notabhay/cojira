package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompactWhitespace(t *testing.T) {
	assert.Equal(t, "hello world from cojira", compactWhitespace("  hello\nworld\t from   cojira "))
}

func TestFormatHumanTimestamp(t *testing.T) {
	assert.Equal(t, "2026-04-16 10:30", formatHumanTimestamp("2026-04-16T10:30:45.000+0000"))
	assert.Equal(t, "-", formatHumanTimestamp(""))
}

func TestFormatHumanBytes(t *testing.T) {
	assert.Equal(t, "512 B", formatHumanBytes(512))
	assert.Equal(t, "2.0 KB", formatHumanBytes(2048))
	assert.Equal(t, "-", formatHumanBytes(nil))
}
