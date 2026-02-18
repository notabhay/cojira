package output

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var isoRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)

// --- Envelope tests ---

func TestEnvelopeRequiredFields(t *testing.T) {
	data := BuildEnvelope(true, "jira", "info", nil, map[string]any{"x": 1}, nil, nil, "", "", "", nil)
	for _, key := range []string{"schema_version", "mode", "ok", "command", "tool", "result", "timestamp", "exit_code"} {
		_, ok := data[key]
		assert.True(t, ok, "missing key %q", key)
	}
}

func TestEnvelopeModeDefault(t *testing.T) {
	_ = os.Unsetenv("COJIRA_OUTPUT_MODE")
	SetMode("")
	data := BuildEnvelope(true, "jira", "info", nil, map[string]any{}, nil, nil, "", "", "", nil)
	assert.Equal(t, "human", data["mode"])
}

func TestEnvelopeTimestampFormat(t *testing.T) {
	data := BuildEnvelope(true, "jira", "info", nil, map[string]any{}, nil, nil, "", "", "", nil)
	ts, ok := data["timestamp"].(string)
	require.True(t, ok)
	assert.Regexp(t, isoRe, ts)
}

func TestEnvelopeWarningsEmptyByDefault(t *testing.T) {
	data := BuildEnvelope(true, "jira", "info", nil, map[string]any{}, nil, nil, "", "", "", nil)
	assert.Equal(t, []any{}, data["warnings"])
}

func TestEnvelopeWarningsPopulated(t *testing.T) {
	warn := map[string]any{"code": "X", "message": "m"}
	data := BuildEnvelope(true, "jira", "info", nil, map[string]any{}, []any{warn}, nil, "", "", "", nil)
	warnings := data["warnings"].([]any)
	assert.Len(t, warnings, 1)
	assert.Equal(t, warn, warnings[0])
}

func TestEnvelopeExitCodeDefaultOK(t *testing.T) {
	data := BuildEnvelope(true, "jira", "info", nil, nil, nil, nil, "", "", "", nil)
	assert.Equal(t, 0, data["exit_code"])
}

func TestEnvelopeExitCodeDefaultFail(t *testing.T) {
	data := BuildEnvelope(false, "jira", "info", nil, nil, nil, nil, "", "", "", nil)
	assert.Equal(t, 1, data["exit_code"])
}

func TestEnvelopeExitCodeExplicit(t *testing.T) {
	ec := 2
	data := BuildEnvelope(false, "jira", "info", nil, nil, nil, nil, "", "", "", &ec)
	assert.Equal(t, 2, data["exit_code"])
}

// --- PrintJSON tests ---

func TestPrintJSONOutput(t *testing.T) {
	_ = os.Setenv("COJIRA_OUTPUT_MODE", "json")
	defer func() { _ = os.Unsetenv("COJIRA_OUTPUT_MODE") }()
	SetMode("")

	data := BuildEnvelope(
		false, "jira", "info", nil, nil,
		nil,
		[]any{map[string]any{"code": "HTTP_403", "message": "Forbidden", "hint": "nope"}},
		"", "", "", nil,
	)

	s, err := JSONDumps(data)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &parsed))
	assert.Equal(t, false, parsed["ok"])
	errs := parsed["errors"].([]any)
	errObj := errs[0].(map[string]any)
	assert.Equal(t, "HTTP_403", errObj["code"])
}

// --- NewEnvelope (functional options) tests ---

func TestNewEnvelopeDefaults(t *testing.T) {
	_ = os.Unsetenv("COJIRA_OUTPUT_MODE")
	SetMode("")
	e := NewEnvelope(WithOK(true), WithTool("jira"), WithCommand("info"))
	assert.Equal(t, "1.0", e.SchemaVersion)
	assert.Equal(t, "human", e.Mode)
	assert.True(t, e.OK)
	assert.Equal(t, 0, e.ExitCode)
	assert.NotEmpty(t, e.Timestamp)
	assert.Equal(t, map[string]any{}, e.Target)
	assert.Equal(t, []any{}, e.Warnings)
	assert.Equal(t, []any{}, e.Errors)
}

func TestNewEnvelopeOverrides(t *testing.T) {
	e := NewEnvelope(
		WithOK(false),
		WithTool("confluence"),
		WithCommand("get"),
		WithMode("json"),
		WithTimestamp("2024-01-01T00:00:00Z"),
		WithExitCode(2),
		WithTarget(map[string]any{"id": "123"}),
		WithResult("hello"),
		WithWarnings([]any{"w1"}),
		WithErrors([]any{"e1"}),
		WithSchemaVersion("2.0"),
	)
	assert.Equal(t, "2.0", e.SchemaVersion)
	assert.Equal(t, "json", e.Mode)
	assert.False(t, e.OK)
	assert.Equal(t, 2, e.ExitCode)
	assert.Equal(t, "2024-01-01T00:00:00Z", e.Timestamp)
	assert.Equal(t, "confluence", e.Tool)
	assert.Equal(t, "get", e.Command)
	assert.Equal(t, map[string]any{"id": "123"}, e.Target)
	assert.Equal(t, "hello", e.Result)
	assert.Equal(t, []any{"w1"}, e.Warnings)
	assert.Equal(t, []any{"e1"}, e.Errors)
}

// --- Receipt tests ---

func TestReceiptOKFormat(t *testing.T) {
	r := &Receipt{OK: true, Message: "did thing"}
	formatted := r.Format()
	assert.Contains(t, formatted, "[OK] did thing at ")
	assert.Regexp(t, isoRe, formatted)
}

func TestReceiptFailedFormat(t *testing.T) {
	r := &Receipt{OK: false, Message: "broke"}
	formatted := r.Format()
	assert.Contains(t, formatted, "[FAILED] broke at ")
	assert.Regexp(t, isoRe, formatted)
}

func TestReceiptDryRunFormat(t *testing.T) {
	r := &Receipt{OK: true, DryRun: true, Message: "would do"}
	formatted := r.Format()
	assert.Contains(t, formatted, "[DRY-RUN] would do at ")
	assert.Regexp(t, isoRe, formatted)
}

func TestReceiptWithChanges(t *testing.T) {
	r := &Receipt{
		OK:        true,
		Message:   "updated",
		Timestamp: "2024-01-01T00:00:00Z",
		Changes: []Change{
			{Field: "priority", OldValue: "Low", NewValue: "High"},
		},
	}
	formatted := r.Format()
	assert.Contains(t, formatted, "[OK] updated at 2024-01-01T00:00:00Z")
	assert.Contains(t, formatted, "  priority: Low -> High")
}

// --- IdempotencyKey tests ---

func TestIdempotencyKeyDeterministic(t *testing.T) {
	k1 := IdempotencyKey("a", 1)
	k2 := IdempotencyKey("a", 1)
	k3 := IdempotencyKey("a", 2)
	assert.Equal(t, k1, k2)
	assert.NotEqual(t, k1, k3)
}

// --- RequestID tests ---

func TestRequestIDFormat(t *testing.T) {
	rid := RequestID()
	assert.Len(t, rid, 16)
	// Should be valid hex.
	for _, c := range rid {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'), "char %c not hex", c)
	}
}

func TestRequestIDUnique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := RequestID()
		assert.False(t, ids[id], "duplicate request ID: %s", id)
		ids[id] = true
	}
}

// --- Mode tests ---

func TestSetAndGetMode(t *testing.T) {
	SetMode("json")
	assert.Equal(t, "json", GetMode())
	SetMode("")
	_ = os.Unsetenv("COJIRA_OUTPUT_MODE")
	assert.Equal(t, "human", GetMode())
}

func TestGetModeFromEnv(t *testing.T) {
	SetMode("")
	_ = os.Setenv("COJIRA_OUTPUT_MODE", "summary")
	defer func() { _ = os.Unsetenv("COJIRA_OUTPUT_MODE") }()
	assert.Equal(t, "summary", GetMode())
}

// --- ErrorObj tests ---

func TestErrorObjValid(t *testing.T) {
	obj, err := ErrorObj("HTTP_403", "Forbidden", "check perms", "", nil)
	require.NoError(t, err)
	assert.Equal(t, "HTTP_403", obj["code"])
	assert.Equal(t, "Forbidden", obj["message"])
	assert.Equal(t, "check perms", obj["hint"])
	// user_message should come from defaults
	_, hasUM := obj["user_message"]
	assert.True(t, hasUM)
}

func TestErrorObjUnknownCode(t *testing.T) {
	_, err := ErrorObj("NOT_A_CODE", "msg", "", "", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown error code")
}

// --- UTCNowISO tests ---

func TestUTCNowISOFormat(t *testing.T) {
	ts := UTCNowISO()
	assert.Regexp(t, isoRe, ts)
}
