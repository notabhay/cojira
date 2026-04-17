package output

import (
	"sync"

	"github.com/notabhay/cojira/internal/events"
)

var (
	eventMu       sync.RWMutex
	eventStreamID string
)

// SetEventStreamID sets the current event stream id for this process.
func SetEventStreamID(id string) {
	eventMu.Lock()
	eventStreamID = id
	eventMu.Unlock()
}

// CurrentEventStreamID returns the current event stream id.
func CurrentEventStreamID() string {
	eventMu.RLock()
	id := eventStreamID
	eventMu.RUnlock()
	return id
}

func ensureEventStreamID() string {
	eventMu.Lock()
	defer eventMu.Unlock()
	if eventStreamID == "" {
		eventStreamID = RequestID()
	}
	return eventStreamID
}

// EmitEvent persists a structured event to the current event stream.
func EmitEvent(kind string, payload map[string]any) string {
	id := ensureEventStreamID()
	if payload == nil {
		payload = map[string]any{}
	}
	payload["type"] = kind
	if _, ok := payload["timestamp"]; !ok {
		payload["timestamp"] = UTCNowISO()
	}
	payload["stream_id"] = id
	_, _ = events.Append(id, payload)
	return id
}

// EmitError persists a structured error event to the current event stream.
func EmitError(code string, message string, payload map[string]any) string {
	if payload == nil {
		payload = map[string]any{}
	}
	if code != "" {
		payload["code"] = code
	}
	payload["message"] = message
	return EmitEvent("error", payload)
}
