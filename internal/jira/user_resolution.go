package jira

import (
	"fmt"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
)

func resolveUserReference(client *Client, ref string) (map[string]any, error) {
	value := strings.TrimSpace(ref)
	if value == "" {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "User reference cannot be empty.",
			ExitCode: 1,
		}
	}

	switch strings.ToLower(value) {
	case "me", "self", "currentuser()":
		return client.GetMyself()
	}

	if key, raw := splitTypedUserRef(value); key != "" {
		return map[string]any{key: raw}, nil
	}

	users, err := client.SearchUsers(value, 20)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.IdentUnresolved,
			Message:  fmt.Sprintf("No Jira users found for %q.", value),
			ExitCode: 1,
		}
	}

	if exact := exactUserMatches(users, value); len(exact) == 1 {
		return exact[0], nil
	}
	if len(users) == 1 {
		return users[0], nil
	}

	suggestions := make([]string, 0, 5)
	for _, user := range users {
		suggestions = append(suggestions, formatUserDisplay(user))
		if len(suggestions) == 5 {
			break
		}
	}

	return nil, &cerrors.CojiraError{
		Code:     cerrors.OpFailed,
		Message:  fmt.Sprintf("User reference %q matched multiple Jira users: %s", value, strings.Join(suggestions, "; ")),
		ExitCode: 1,
	}
}

func splitTypedUserRef(value string) (string, string) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}

	prefix := strings.ToLower(strings.TrimSpace(parts[0]))
	raw := strings.TrimSpace(parts[1])
	if raw == "" {
		return "", ""
	}

	switch prefix {
	case "accountid", "account":
		return "accountId", raw
	case "name", "username":
		return "name", raw
	case "key", "userkey":
		return "key", raw
	case "email", "emailaddress":
		return "emailAddress", raw
	default:
		return "", ""
	}
}

func exactUserMatches(users []map[string]any, ref string) []map[string]any {
	needle := strings.ToLower(strings.TrimSpace(ref))
	if needle == "" {
		return nil
	}

	var matches []map[string]any
	for _, user := range users {
		for _, key := range []string{"accountId", "name", "key", "displayName", "emailAddress"} {
			if strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", user[key])), needle) {
				matches = append(matches, user)
				break
			}
		}
	}
	return matches
}

func formatUserDisplay(user map[string]any) string {
	name := strings.TrimSpace(fmt.Sprintf("%v", user["displayName"]))
	if name == "" || name == "<nil>" {
		name = strings.TrimSpace(fmt.Sprintf("%v", user["name"]))
	}
	email := strings.TrimSpace(fmt.Sprintf("%v", user["emailAddress"]))
	accountID := strings.TrimSpace(fmt.Sprintf("%v", user["accountId"]))
	if name == "" || name == "<nil>" {
		if email != "" && email != "<nil>" {
			name = email
		} else if accountID != "" && accountID != "<nil>" {
			name = accountID
		}
	}

	switch {
	case email != "" && email != "<nil>":
		if name == email {
			return email
		}
		return fmt.Sprintf("%s <%s>", name, email)
	case accountID != "" && accountID != "<nil>":
		return fmt.Sprintf("%s [%s]", name, accountID)
	default:
		return name
	}
}

func jiraUserAssignmentPayload(user map[string]any) map[string]any {
	for _, key := range []string{"accountId", "name", "key"} {
		if value := strings.TrimSpace(fmt.Sprintf("%v", user[key])); value != "" && value != "<nil>" {
			return map[string]any{key: value}
		}
	}
	return map[string]any{}
}
