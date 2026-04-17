package jira

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/markdownconv"
)

func jiraUsesADF() bool {
	return strings.TrimSpace(strings.ToLower(os.Getenv("JIRA_API_VERSION"))) == "3"
}

func normalizeJiraRichTextValue(raw, format string, useADF bool) (any, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "raw"
	}

	if !useADF {
		switch format {
		case "raw", "text", "jira", "jira-wiki":
			return raw, nil
		case "markdown", "md":
			converted, err := markdownconv.ToJiraWiki(raw)
			if err != nil {
				return nil, &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  fmt.Sprintf("Failed to convert Markdown to Jira wiki: %v", err),
					ExitCode: 1,
				}
			}
			return converted, nil
		default:
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Unsupported Jira text format %q. Use raw, markdown, or adf.", format),
				ExitCode: 2,
			}
		}
	}

	switch format {
	case "raw", "text", "jira", "jira-wiki":
		return markdownconv.PlainTextToJiraADF(raw), nil
	case "markdown", "md":
		return markdownconv.ToJiraADF(raw)
	case "adf", "jira-adf":
		var parsed map[string]any
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  fmt.Sprintf("Invalid ADF JSON: %v", err),
				ExitCode: 1,
			}
		}
		return parsed, nil
	default:
		return nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Unsupported Jira text format %q. Use raw, markdown, or adf.", format),
			ExitCode: 2,
		}
	}
}

func normalizeJiraDescriptionField(fields map[string]any, format string, useADF bool) error {
	raw, ok := fields["description"]
	if !ok {
		return nil
	}
	if _, ok := raw.(map[string]any); ok {
		return nil
	}
	textValue, ok := raw.(string)
	if !ok {
		return nil
	}
	converted, err := normalizeJiraRichTextValue(textValue, format, useADF)
	if err != nil {
		return err
	}
	fields["description"] = converted
	return nil
}

func normalizeADFPayload(payload map[string]any) error {
	fields, _ := payload["fields"].(map[string]any)
	if fields == nil {
		return nil
	}
	return normalizeJiraDescriptionField(fields, "raw", true)
}

func plainTextADFDocument(text string) map[string]any {
	return markdownconv.PlainTextToJiraADF(text)
}
