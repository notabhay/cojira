package output

import (
	"fmt"
	"strings"
)

// Change represents a single field change (field, old value, new value).
type Change struct {
	Field    string
	OldValue string
	NewValue string
}

// Receipt is a human-readable summary of an operation result.
type Receipt struct {
	OK        bool
	Message   string
	DryRun    bool
	Timestamp string
	Changes   []Change
}

// Format renders the receipt as a human-readable string.
func (r *Receipt) Format() string {
	ts := r.Timestamp
	if ts == "" {
		ts = UTCNowISO()
	}

	var status string
	switch {
	case r.DryRun:
		status = "DRY-RUN"
	case r.OK:
		status = "OK"
	default:
		status = "FAILED"
	}

	lines := []string{fmt.Sprintf("[%s] %s at %s", status, r.Message, ts)}
	for _, c := range r.Changes {
		lines = append(lines, fmt.Sprintf("  %s: %s -> %s", c.Field, c.OldValue, c.NewValue))
	}
	return strings.Join(lines, "\n")
}
