// Package errors defines the cojira error model, error codes, hints,
// user-facing messages, and recovery actions.
package errors

const (
	// ConfigMissingEnv indicates that required environment or credential values are missing.
	ConfigMissingEnv = "CONFIG_MISSING_ENV"
	// ConfigInvalid indicates that user-provided configuration is malformed.
	ConfigInvalid = "CONFIG_INVALID"
	// HTTPError indicates a generic HTTP failure that does not have a more specific mapped code.
	HTTPError = "HTTP_ERROR"
	// HTTP401 indicates an authentication failure.
	HTTP401 = "HTTP_401"
	// HTTP403 indicates an authorization failure.
	HTTP403 = "HTTP_403"
	// HTTP404 indicates that the requested resource or base URL could not be found.
	HTTP404 = "HTTP_404"
	// HTTP429 indicates server-side rate limiting.
	HTTP429 = "HTTP_429"
	// Timeout indicates that the request exceeded the configured timeout.
	Timeout = "TIMEOUT"
	// IdentUnresolved indicates that a user-supplied identifier could not be resolved.
	IdentUnresolved = "IDENT_UNRESOLVED"
	// FetchFailed indicates that a read operation failed.
	FetchFailed = "FETCH_FAILED"
	// UpdateFailed indicates that an update operation failed.
	UpdateFailed = "UPDATE_FAILED"
	// CreateFailed indicates that a create operation failed.
	CreateFailed = "CREATE_FAILED"
	// TransitionFailed indicates that a Jira transition could not be applied.
	TransitionFailed = "TRANSITION_FAILED"
	// FileNotFound indicates that a referenced local file could not be read.
	FileNotFound = "FILE_NOT_FOUND"
	// InvalidJSON indicates that an input JSON document is malformed.
	InvalidJSON = "INVALID_JSON"
	// OpFailed indicates a generic operation failure.
	OpFailed = "OP_FAILED"
	// Unsupported indicates that the requested action is not supported by cojira.
	Unsupported = "UNSUPPORTED"
	// ConfigError indicates a generic configuration failure.
	ConfigError = "CONFIG_ERROR"
	// Error indicates an unexpected generic failure.
	Error = "ERROR"
	// EmptyContent indicates an attempt to write empty Confluence content.
	EmptyContent = "EMPTY_CONTENT"
	// MoveFailed indicates that a Confluence move operation failed.
	MoveFailed = "MOVE_FAILED"
	// RenameFailed indicates that a Confluence rename operation failed.
	RenameFailed = "RENAME_FAILED"
	// SearchFailed indicates that a search or query operation failed.
	SearchFailed = "SEARCH_FAILED"
	// LabelFailed indicates that a label mutation failed.
	LabelFailed = "LABEL_FAILED"
	// CopyFailed indicates that a copy operation failed.
	CopyFailed = "COPY_FAILED"
	// InvalidTitle indicates that a supplied title is invalid for the target system.
	InvalidTitle = "INVALID_TITLE"
	// CopyLimitation indicates that the platform refused part of a copy flow due to product limitations.
	CopyLimitation = "COPY_LIMITATION"
	// AmbiguousTransition indicates that more than one transition matched the requested target status.
	AmbiguousTransition = "AMBIGUOUS_TRANSITION"
	// TransitionNotFound indicates that the requested transition does not exist for the issue.
	TransitionNotFound = "TRANSITION_NOT_FOUND"
	// MissingDep indicates that a required local dependency is unavailable.
	MissingDep = "MISSING_DEP"
)

// ErrorCodes is the set of all known error codes.
var ErrorCodes = map[string]struct{}{
	ConfigMissingEnv:    {},
	ConfigInvalid:       {},
	HTTPError:           {},
	HTTP401:             {},
	HTTP403:             {},
	HTTP404:             {},
	HTTP429:             {},
	Timeout:             {},
	IdentUnresolved:     {},
	FetchFailed:         {},
	UpdateFailed:        {},
	CreateFailed:        {},
	TransitionFailed:    {},
	FileNotFound:        {},
	InvalidJSON:         {},
	OpFailed:            {},
	Unsupported:         {},
	ConfigError:         {},
	Error:               {},
	EmptyContent:        {},
	MoveFailed:          {},
	RenameFailed:        {},
	SearchFailed:        {},
	LabelFailed:         {},
	CopyFailed:          {},
	InvalidTitle:        {},
	CopyLimitation:      {},
	AmbiguousTransition: {},
	TransitionNotFound:  {},
	MissingDep:          {},
}

// IsValidCode returns true if code is a known error code.
func IsValidCode(code string) bool {
	_, ok := ErrorCodes[code]
	return ok
}
