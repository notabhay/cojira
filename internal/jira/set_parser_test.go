package jira

import (
	"testing"

	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSetBasicEquals(t *testing.T) {
	field, op, value, err := ParseSetExpr("summary=New title")
	require.NoError(t, err)
	assert.Equal(t, "summary", field)
	assert.Equal(t, "=", op)
	assert.Equal(t, "New title", value)
}

func TestParseSetJSONTyped(t *testing.T) {
	field, op, value, err := ParseSetExpr(`priority:={"name":"High"}`)
	require.NoError(t, err)
	assert.Equal(t, "priority", field)
	assert.Equal(t, ":=", op)
	assert.Equal(t, `{"name":"High"}`, value)
}

func TestParseSetListAppend(t *testing.T) {
	field, op, value, err := ParseSetExpr("labels+=urgent")
	require.NoError(t, err)
	assert.Equal(t, "labels", field)
	assert.Equal(t, "+=", op)
	assert.Equal(t, "urgent", value)
}

func TestParseSetListRemove(t *testing.T) {
	field, op, value, err := ParseSetExpr("labels-=stale")
	require.NoError(t, err)
	assert.Equal(t, "labels", field)
	assert.Equal(t, "-=", op)
	assert.Equal(t, "stale", value)
}

func TestParseSetValueContainingEquals(t *testing.T) {
	field, op, value, err := ParseSetExpr("summary=foo=bar=baz")
	require.NoError(t, err)
	assert.Equal(t, "summary", field)
	assert.Equal(t, "=", op)
	assert.Equal(t, "foo=bar=baz", value)
}

func TestParseSetValueContainingColonEquals(t *testing.T) {
	field, op, value, err := ParseSetExpr("summary=text:=notjson")
	require.NoError(t, err)
	assert.Equal(t, "summary=text", field)
	assert.Equal(t, ":=", op)
	assert.Equal(t, "notjson", value)
}

func TestParseSetInvalidNoOperator(t *testing.T) {
	_, _, _, err := ParseSetExpr("justAString")
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.ErrorAs(t, err, &ce)
}

func TestParseSetEmpty(t *testing.T) {
	_, _, _, err := ParseSetExpr("")
	require.Error(t, err)
}

func TestMergeListByNameAppend(t *testing.T) {
	current := []map[string]any{{"name": "Frontend"}}
	result, err := MergeListByName(current, "+=", "Backend")
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "Backend", result[1]["name"])
}

func TestMergeListByNameAppendDuplicate(t *testing.T) {
	current := []map[string]any{{"name": "Frontend"}}
	result, err := MergeListByName(current, "+=", "Frontend")
	require.NoError(t, err)
	assert.Len(t, result, 1)
}

func TestMergeListByNameRemove(t *testing.T) {
	current := []map[string]any{{"name": "Frontend"}, {"name": "Backend"}}
	result, err := MergeListByName(current, "-=", "Frontend")
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "Backend", result[0]["name"])
}

func TestMergeListOfStringsAppend(t *testing.T) {
	current := []string{"a", "b"}
	result, err := MergeListOfStrings(current, "+=", "c")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestMergeListOfStringsAppendDuplicate(t *testing.T) {
	current := []string{"a", "b"}
	result, err := MergeListOfStrings(current, "+=", "b")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, result)
}

func TestMergeListOfStringsRemove(t *testing.T) {
	current := []string{"a", "b", "c"}
	result, err := MergeListOfStrings(current, "-=", "b")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "c"}, result)
}

func TestMergeListBadOp(t *testing.T) {
	_, err := MergeListByName(nil, "~=", "x")
	require.Error(t, err)

	_, err = MergeListOfStrings(nil, "~=", "x")
	require.Error(t, err)
}
