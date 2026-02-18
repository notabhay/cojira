// Package output provides the cojira structured output model —
// envelopes, receipts, progress, JSON formatting, idempotency keys, etc.
package output

import (
	"fmt"
	"time"

	"github.com/cojira/cojira/internal/errors"
)

// UTCNowISO returns the current time in UTC, truncated to seconds,
// formatted as ISO-8601 with a trailing "Z".
func UTCNowISO() string {
	return time.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
}

// Envelope is the standard JSON response wrapper for every cojira command.
type Envelope struct {
	SchemaVersion string         `json:"schema_version"`
	Mode          string         `json:"mode"`
	OK            bool           `json:"ok"`
	Tool          string         `json:"tool"`
	Command       string         `json:"command"`
	Target        map[string]any `json:"target"`
	Result        any            `json:"result"`
	Warnings      []any          `json:"warnings"`
	Errors        []any          `json:"errors"`
	Timestamp     string         `json:"timestamp"`
	ExitCode      int            `json:"exit_code"`
}

// EnvelopeOption is a functional option for building an Envelope.
type EnvelopeOption func(*Envelope)

// WithOK sets the ok field.
func WithOK(ok bool) EnvelopeOption {
	return func(e *Envelope) { e.OK = ok }
}

// WithTool sets the tool field.
func WithTool(tool string) EnvelopeOption {
	return func(e *Envelope) { e.Tool = tool }
}

// WithCommand sets the command field.
func WithCommand(command string) EnvelopeOption {
	return func(e *Envelope) { e.Command = command }
}

// WithTarget sets the target field.
func WithTarget(target map[string]any) EnvelopeOption {
	return func(e *Envelope) { e.Target = target }
}

// WithResult sets the result field.
func WithResult(result any) EnvelopeOption {
	return func(e *Envelope) { e.Result = result }
}

// WithWarnings sets the warnings field.
func WithWarnings(warnings []any) EnvelopeOption {
	return func(e *Envelope) { e.Warnings = warnings }
}

// WithErrors sets the errors field.
func WithErrors(errs []any) EnvelopeOption {
	return func(e *Envelope) { e.Errors = errs }
}

// WithTimestamp overrides the auto-generated timestamp.
func WithTimestamp(ts string) EnvelopeOption {
	return func(e *Envelope) { e.Timestamp = ts }
}

// WithMode overrides the output mode (default comes from GetMode).
func WithMode(mode string) EnvelopeOption {
	return func(e *Envelope) { e.Mode = mode }
}

// WithSchemaVersion overrides the schema version (default "1.0").
func WithSchemaVersion(v string) EnvelopeOption {
	return func(e *Envelope) { e.SchemaVersion = v }
}

// WithExitCode explicitly sets the exit code.
// If not set, it defaults to 0 when ok=true and 1 when ok=false.
func WithExitCode(code int) EnvelopeOption {
	return func(e *Envelope) { e.ExitCode = code }
}

// NewEnvelope builds an Envelope by applying the given options.
// The required fields ok, tool, and command should be provided via options.
func NewEnvelope(opts ...EnvelopeOption) *Envelope {
	e := &Envelope{
		SchemaVersion: "1.0",
		ExitCode:      -1, // sentinel; resolved below
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.Mode == "" {
		e.Mode = GetMode()
	}
	if e.Target == nil {
		e.Target = map[string]any{}
	}
	if e.Warnings == nil {
		e.Warnings = []any{}
	}
	if e.Errors == nil {
		e.Errors = []any{}
	}
	if e.Timestamp == "" {
		e.Timestamp = UTCNowISO()
	}
	if e.ExitCode == -1 {
		if e.OK {
			e.ExitCode = 0
		} else {
			e.ExitCode = 1
		}
	}
	return e
}

// BuildEnvelope is a convenience wrapper matching the Python envelope() signature.
func BuildEnvelope(
	ok bool,
	tool string,
	command string,
	target map[string]any,
	result any,
	warnings []any,
	errs []any,
	timestamp string,
	mode string,
	schemaVersion string,
	exitCode *int,
) map[string]any {
	if schemaVersion == "" {
		schemaVersion = "1.0"
	}
	modeValue := mode
	if modeValue == "" {
		modeValue = GetMode()
	}
	ec := 0
	if exitCode != nil {
		ec = *exitCode
	} else if !ok {
		ec = 1
	}
	if target == nil {
		target = map[string]any{}
	}
	w := compactList(warnings)
	e := compactList(errs)
	ts := timestamp
	if ts == "" {
		ts = UTCNowISO()
	}
	return map[string]any{
		"schema_version": schemaVersion,
		"mode":           modeValue,
		"ok":             ok,
		"tool":           tool,
		"command":        command,
		"target":         target,
		"result":         result,
		"warnings":       w,
		"errors":         e,
		"timestamp":      ts,
		"exit_code":      ec,
	}
}

func compactList(values []any) []any {
	if values == nil {
		return []any{}
	}
	return values
}

// ErrorObj builds a structured error object matching the Python error_obj() function.
// It validates the error code against the known set.
func ErrorObj(code, message string, hint string, userMessage string, recovery map[string]any) (map[string]any, error) {
	if !errors.IsValidCode(code) {
		return nil, fmt.Errorf("unknown error code: %s", code)
	}
	if userMessage == "" {
		userMessage = errors.DefaultUserMessage(code, message)
	}
	if recovery == nil {
		recovery = errors.DefaultRecovery(code)
	}
	out := map[string]any{
		"code":    code,
		"message": message,
	}
	if hint != "" {
		out["hint"] = hint
	}
	if userMessage != "" {
		out["user_message"] = userMessage
	}
	if recovery != nil {
		out["recovery"] = recovery
	}
	return out, nil
}
