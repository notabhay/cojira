package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveIssueIdentifier(t *testing.T) {
	assert.Equal(t, "PROJ-123", ResolveIssueIdentifier("PROJ-123"))
	assert.Equal(t, "12345", ResolveIssueIdentifier("12345"))
	assert.Equal(t, "PROJ-123", ResolveIssueIdentifier("/browse/PROJ-123"))
	assert.Equal(t, "PROJ-123",
		ResolveIssueIdentifier("https://jira.example.com/jira/browse/PROJ-123"))
	assert.Equal(t, "PROJ-123",
		ResolveIssueIdentifier("https://jira.example.com/jira/rest/api/2/issue/PROJ-123"))
	assert.Equal(t, "PROJ-9",
		ResolveIssueIdentifier("https://jira.example.com/jira/secure/RapidBoard.jspa?selectedIssue=PROJ-9"))
}

func TestInferBaseURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"https://jira.example.com/jira/browse/PROJ-123",
			"https://jira.example.com/jira",
		},
		{
			"https://jira.example.com/jira/rest/api/2/issue/PROJ-123",
			"https://jira.example.com/jira",
		},
		{
			"https://jira.example.com/jira/secure/RapidBoard.jspa",
			"https://jira.example.com/jira",
		},
		{
			"https://jira.example.com/jira/projects/PROJ/board",
			"https://jira.example.com/jira",
		},
		{
			"https://jira.example.com/jira/plugins/servlet/personal-access-tokens",
			"https://jira.example.com/jira",
		},
		// Jira Software next-gen: company-managed.
		{
			"https://jira.example.com/jira/software/c/projects/PROJ/boards/123",
			"https://jira.example.com/jira",
		},
		// Jira Software next-gen: team-managed.
		{
			"https://jira.example.com/jira/software/projects/PROJ/boards/123",
			"https://jira.example.com/jira",
		},
		// Root deployment (no context path).
		{
			"https://jira.example.com/software/c/projects/PROJ/boards/123",
			"https://jira.example.com",
		},
		{
			"https://jira.example.com/software/projects/PROJ/board",
			"https://jira.example.com",
		},
		// Edge case: context path IS /software — doubled /software/software/.
		{
			"https://jira.example.com/software/software/c/projects/PROJ/boards/123",
			"https://jira.example.com/software",
		},
		{
			"https://jira.example.com/software/software/projects/PROJ/boards/123",
			"https://jira.example.com/software",
		},
		// Non-URL returns empty.
		{
			"PROJ-123",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := InferBaseURL(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
