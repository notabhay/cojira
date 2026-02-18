package errors

// CojiraError is the structured error type used throughout cojira.
type CojiraError struct {
	Code        string         `json:"code"`
	Message     string         `json:"message"`
	Hint        string         `json:"hint,omitempty"`
	UserMessage string         `json:"user_message,omitempty"`
	Recovery    map[string]any `json:"recovery,omitempty"`
	ExitCode    int            `json:"exit_code"`
}

// Error implements the error interface.
func (e *CojiraError) Error() string {
	return e.Message
}

// New creates a CojiraError with the given code and message.
// ExitCode defaults to 1.
func New(code, message string) *CojiraError {
	return &CojiraError{
		Code:     code,
		Message:  message,
		ExitCode: 1,
	}
}
