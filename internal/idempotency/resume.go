package idempotency

// ResumeItem describes one completed or remaining unit in a resumable
// multi-item operation.
type ResumeItem struct {
	ID          string         `json:"id"`
	Description string         `json:"description,omitempty"`
	Target      map[string]any `json:"target,omitempty"`
}

// ResumeState is a machine-readable payload emitted on partial multi-item
// failures so agents can safely resume without replaying succeeded items.
type ResumeState struct {
	Version        int            `json:"version"`
	Kind           string         `json:"kind"`
	IdempotencyKey string         `json:"idempotency_key"`
	RequestID      string         `json:"request_id,omitempty"`
	Target         map[string]any `json:"target,omitempty"`
	Snapshot       any            `json:"snapshot,omitempty"`
	Completed      []ResumeItem   `json:"completed,omitempty"`
	Remaining      []ResumeItem   `json:"remaining,omitempty"`
	ResumeHint     string         `json:"resume_hint,omitempty"`
	Notes          []string       `json:"notes,omitempty"`
}

// NewResumeState constructs a resumable-state payload with the current schema
// version and the shared rerun hint.
func NewResumeState(kind, idempotencyKey, requestID string, target map[string]any, snapshot any) ResumeState {
	return ResumeState{
		Version:        1,
		Kind:           kind,
		IdempotencyKey: idempotencyKey,
		RequestID:      requestID,
		Target:         target,
		Snapshot:       snapshot,
		ResumeHint:     "Rerun the same command with the emitted idempotency key to resume from the frozen operation snapshot.",
	}
}
