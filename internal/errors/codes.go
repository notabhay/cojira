// Package errors defines the cojira error model, error codes, hints,
// user-facing messages, and recovery actions.
package errors

// Error code constants.
const (
	ConfigMissingEnv    = "CONFIG_MISSING_ENV"
	ConfigInvalid       = "CONFIG_INVALID"
	HTTPError           = "HTTP_ERROR"
	HTTP401             = "HTTP_401"
	HTTP403             = "HTTP_403"
	HTTP404             = "HTTP_404"
	HTTP429             = "HTTP_429"
	Timeout             = "TIMEOUT"
	IdentUnresolved     = "IDENT_UNRESOLVED"
	FetchFailed         = "FETCH_FAILED"
	UpdateFailed        = "UPDATE_FAILED"
	CreateFailed        = "CREATE_FAILED"
	TransitionFailed    = "TRANSITION_FAILED"
	FileNotFound        = "FILE_NOT_FOUND"
	InvalidJSON         = "INVALID_JSON"
	OpFailed            = "OP_FAILED"
	Unsupported         = "UNSUPPORTED"
	ConfigError         = "CONFIG_ERROR"
	Error               = "ERROR"
	EmptyContent        = "EMPTY_CONTENT"
	MoveFailed          = "MOVE_FAILED"
	RenameFailed        = "RENAME_FAILED"
	SearchFailed        = "SEARCH_FAILED"
	LabelFailed         = "LABEL_FAILED"
	CopyFailed          = "COPY_FAILED"
	InvalidTitle        = "INVALID_TITLE"
	CopyLimitation      = "COPY_LIMITATION"
	AmbiguousTransition = "AMBIGUOUS_TRANSITION"
	TransitionNotFound  = "TRANSITION_NOT_FOUND"
	MissingDep          = "MISSING_DEP"
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
