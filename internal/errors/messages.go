package errors

import "strings"

// userMessages maps error codes to user-friendly messages.
var userMessages = map[string]string{
	ConfigMissingEnv:    "Setup is incomplete. Run `cojira doctor` to diagnose or `cojira init` to configure credentials.",
	ConfigInvalid:       "Your configuration looks invalid. Run `cojira doctor` to diagnose or re-run `cojira init`.",
	HTTP401:             "Your token doesn't have permission for this. Create a new token with the right access.",
	HTTP403:             "Your token doesn't have permission for this. Create a new token with the right access.",
	HTTP404:             "The URL returned 'not found'. Check that JIRA_BASE_URL includes any context path (e.g. /jira).",
	HTTP429:             "Jira asked us to slow down (rate limit). Please wait a bit and try again.",
	HTTPError:           "Something went wrong talking to the server. Try again in a moment.",
	Timeout:             "The request timed out. Try again or check your network connection.",
	IdentUnresolved:     "I couldn't find that item. Share a full URL or ID and try again.",
	FetchFailed:         "I couldn't retrieve that item. Check the ID/URL and try again.",
	UpdateFailed:        "The update didn't go through. The page may have been edited by someone else — try again.",
	CreateFailed:        "I couldn't create that item. Check your permissions and try again.",
	TransitionFailed:    "I couldn't change the status. The transition may not be allowed from the current state.",
	AmbiguousTransition: "Multiple transitions matched that status. Pick a specific transition ID and try again.",
	TransitionNotFound:  "That status transition isn't available for this issue.",
	FileNotFound:        "I couldn't find the file you referenced. Check the path and try again.",
	InvalidJSON:         "That JSON file isn't valid. Fix the file and retry.",
	EmptyContent:        "The content is empty. Provide some content and try again.",
	MoveFailed:          "I couldn't move that page. Check permissions and that the target parent exists.",
	RenameFailed:        "I couldn't rename that page. The title may already be taken.",
	SearchFailed:        "The search didn't work. Check your query syntax and try again.",
	LabelFailed:         "I couldn't update the labels. Check permissions.",
	CopyFailed:          "The copy operation failed. Some pages may not have been copied.",
	InvalidTitle:        "That page title isn't valid. Titles can't be empty or contain certain special characters.",
	CopyLimitation:      "Some items couldn't be copied due to Confluence limitations.",
	Unsupported:         "That operation isn't supported yet.",
	ConfigError:         "There's a problem with your configuration. Run `cojira doctor` to diagnose.",
	Error:               "Something unexpected went wrong. Try again or run `cojira doctor`.",
	MissingDep:          "A required package is missing. Check the install instructions.",
}

// DefaultUserMessage returns the user-friendly message for the given error code.
// For OpFailed, it returns the message itself if it's short enough (<=160 chars).
// Returns empty string if no default message is available.
func DefaultUserMessage(code string, message string) string {
	if msg, ok := userMessages[code]; ok {
		return msg
	}
	if code == OpFailed && message != "" {
		short := strings.TrimSpace(message)
		if len(short) > 0 && len(short) <= 160 {
			return short
		}
	}
	return ""
}

// recoveries maps error codes to recovery action descriptors.
var recoveries = map[string]map[string]any{
	ConfigMissingEnv:    {"action": "run", "command": "cojira init", "requires_user": true},
	ConfigInvalid:       {"action": "run", "command": "cojira init", "requires_user": true},
	HTTP401:             {"action": "run", "command": "cojira init", "requires_user": true},
	HTTP403:             {"action": "run", "command": "cojira init", "requires_user": true},
	HTTP404:             {"action": "run", "command": "cojira init", "requires_user": true},
	HTTP429:             {"action": "retry", "hint": "wait a bit and retry"},
	Timeout:             {"action": "retry", "flag": "--timeout 60"},
	IdentUnresolved:     {"action": "retry", "hint": "use full URL or numeric ID"},
	FetchFailed:         {"action": "retry"},
	UpdateFailed:        {"action": "retry", "hint": "fetch latest version first"},
	CreateFailed:        {"action": "check", "command": "cojira doctor"},
	TransitionFailed:    {"action": "run", "command": "cojira jira transitions {issue}"},
	FileNotFound:        {"action": "check", "hint": "verify file path"},
	InvalidJSON:         {"action": "check", "hint": "validate JSON syntax"},
	EmptyContent:        {"action": "check", "hint": "provide non-empty content"},
	MoveFailed:          {"action": "retry"},
	RenameFailed:        {"action": "retry", "hint": "check title uniqueness"},
	SearchFailed:        {"action": "retry", "hint": "check query syntax"},
	LabelFailed:         {"action": "retry"},
	CopyFailed:          {"action": "retry"},
	AmbiguousTransition: {"action": "run", "command": "cojira jira transitions {issue}"},
	TransitionNotFound:  {"action": "run", "command": "cojira jira transitions {issue}"},
	MissingDep:          {"action": "run", "command": "go install github.com/notabhay/cojira@latest"},
	Error:               {"action": "run", "command": "cojira doctor"},
	ConfigError:         {"action": "run", "command": "cojira doctor"},
}

// DefaultRecovery returns the recovery action descriptor for the given error code,
// or nil if no recovery is defined.
func DefaultRecovery(code string) map[string]any {
	r, ok := recoveries[code]
	if !ok {
		return nil
	}
	// Return a copy to prevent mutation.
	cp := make(map[string]any, len(r))
	for k, v := range r {
		cp[k] = v
	}
	return cp
}
