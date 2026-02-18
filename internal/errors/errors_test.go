package errors

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorCodesAreNonEmpty(t *testing.T) {
	require.NotEmpty(t, ErrorCodes)
	for code := range ErrorCodes {
		assert.NotEmpty(t, code, "error code must be non-empty string")
	}
}

func TestIsValidCode(t *testing.T) {
	assert.True(t, IsValidCode(Timeout))
	assert.True(t, IsValidCode(HTTP404))
	assert.False(t, IsValidCode("TYPO_CODE"))
	assert.False(t, IsValidCode(""))
}

func TestCojiraErrorAttributes(t *testing.T) {
	err := &CojiraError{
		Code:        OpFailed,
		Message:     "bad",
		Hint:        "h",
		UserMessage: "u",
		Recovery:    map[string]any{"action": "run"},
		ExitCode:    2,
	}
	assert.Equal(t, OpFailed, err.Code)
	assert.Equal(t, "bad", err.Message)
	assert.Equal(t, "h", err.Hint)
	assert.Equal(t, "u", err.UserMessage)
	assert.Equal(t, map[string]any{"action": "run"}, err.Recovery)
	assert.Equal(t, 2, err.ExitCode)
}

func TestCojiraErrorImplementsError(t *testing.T) {
	err := New(Timeout, "timed out")
	assert.Equal(t, "timed out", err.Error())
	assert.Equal(t, 1, err.ExitCode)
}

func TestHintFunctionsReturnStrings(t *testing.T) {
	assert.NotEmpty(t, HintSetup())

	ts := float64(5)
	assert.NotEmpty(t, HintTimeout(&ts))
	assert.NotEmpty(t, HintTimeout(nil))

	assert.NotEmpty(t, HintIdentifier("x"))
	assert.NotEmpty(t, HintPermission())
	assert.NotEmpty(t, HintBaseURL())
	assert.NotEmpty(t, HintAuthMode())
	assert.NotEmpty(t, HintRateLimit())
}

func TestHintTimeoutIncludesDuration(t *testing.T) {
	ts := float64(5)
	assert.Contains(t, HintTimeout(&ts), "5")
}

func TestHintBaseURLMentionsContextPath(t *testing.T) {
	assert.Contains(t, HintBaseURL(), "context path")
}

func TestHintAuthModeMentionsBearer(t *testing.T) {
	assert.Contains(t, strings.ToLower(HintAuthMode()), "bearer")
}

func TestCoerceHTTPStatusHint(t *testing.T) {
	assert.Equal(t, HintPermission(), CoerceHTTPStatusHint(401))
	assert.Equal(t, HintPermission(), CoerceHTTPStatusHint(403))
	assert.Contains(t, CoerceHTTPStatusHint(404), "context path")
	assert.Equal(t, HintRateLimit(), CoerceHTTPStatusHint(429))
	assert.Empty(t, CoerceHTTPStatusHint(500))
	assert.Empty(t, CoerceHTTPStatusHint(200))
}

func TestAllErrorCodesHaveUserMessage(t *testing.T) {
	for code := range ErrorCodes {
		msg := DefaultUserMessage(code, "short test")
		if code == OpFailed {
			assert.Equal(t, "short test", msg)
		} else {
			assert.NotEmpty(t, msg, "%s has no default user_message", code)
		}
	}
}

func TestOpFailedLongMessageReturnsEmpty(t *testing.T) {
	longMsg := strings.Repeat("x", 200)
	assert.Empty(t, DefaultUserMessage(OpFailed, longMsg))
}

func TestOpFailedShortMessageReturned(t *testing.T) {
	assert.Equal(t, "short test", DefaultUserMessage(OpFailed, "short test"))
}

func TestHTTP404HasUserMessage(t *testing.T) {
	msg := DefaultUserMessage(HTTP404, "")
	require.NotEmpty(t, msg)
	assert.Contains(t, strings.ToLower(msg), "not found")
}

func TestAllExpectedCodesHaveRecovery(t *testing.T) {
	codesWithRecovery := []string{
		ConfigMissingEnv, ConfigInvalid,
		HTTP401, HTTP403, HTTP404, HTTP429,
		Timeout, IdentUnresolved,
		FetchFailed, UpdateFailed,
		CreateFailed, TransitionFailed,
		FileNotFound, InvalidJSON,
		EmptyContent, MoveFailed,
		RenameFailed, SearchFailed,
		LabelFailed, CopyFailed,
		AmbiguousTransition, TransitionNotFound,
		MissingDep, Error, ConfigError,
	}
	for _, code := range codesWithRecovery {
		recovery := DefaultRecovery(code)
		require.NotNil(t, recovery, "%s should have recovery", code)
		assert.Contains(t, recovery, "action", "%s recovery missing 'action' key", code)
	}
}

func TestCodesWithoutRecovery(t *testing.T) {
	codesWithoutRecovery := []string{
		Unsupported, OpFailed, CopyLimitation, InvalidTitle, HTTPError,
	}
	for _, code := range codesWithoutRecovery {
		assert.Nil(t, DefaultRecovery(code), "%s should have no recovery", code)
	}
}

func TestExistingRecoveryValuesUnchanged(t *testing.T) {
	for _, code := range []string{ConfigMissingEnv, ConfigInvalid, HTTP401, HTTP403} {
		recovery := DefaultRecovery(code)
		require.NotNil(t, recovery)
		assert.Equal(t, "cojira init", recovery["command"])
		assert.Equal(t, true, recovery["requires_user"])
	}
}

func TestDefaultRecoveryReturnsCopy(t *testing.T) {
	r1 := DefaultRecovery(Timeout)
	require.NotNil(t, r1)
	r1["mutated"] = true

	r2 := DefaultRecovery(Timeout)
	require.NotNil(t, r2)
	assert.NotContains(t, r2, "mutated", "DefaultRecovery should return independent copies")
}
