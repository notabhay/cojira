package jira

import (
	"strings"
	"testing"
)

func FuzzResolveIssueIdentifier(f *testing.F) {
	seeds := []string{
		"PROJ-123",
		"12345",
		"https://jira.example.com/jira/browse/PROJ-123",
		"https://jira.example.com/jira/rest/api/2/issue/PROJ-123",
		"https://jira.example.com/jira/secure/RapidBoard.jspa?selectedIssue=PROJ-9",
		" /browse/PROJ-123 ",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_ = ResolveIssueIdentifier(input)
	})
}

func FuzzParseSetExpr(f *testing.F) {
	seeds := []string{
		"summary=New title",
		`priority:={"name":"High"}`,
		"labels+=urgent",
		"labels-=stale",
		"fields.summary = hello",
		"justAString",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		field, op, value, err := ParseSetExpr(input)
		if err != nil {
			return
		}
		if strings.TrimSpace(field) == "" {
			t.Fatalf("empty field for successful parse: %q", input)
		}
		switch op {
		case OpJSONSet, OpListAppend, OpListRemove, OpSet:
		default:
			t.Fatalf("unexpected operator %q for input %q", op, input)
		}
		_ = value
	})
}
