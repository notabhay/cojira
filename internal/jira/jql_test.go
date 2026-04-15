package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFixJQLShellEscapesBackslashBang(t *testing.T) {
	assert.Equal(t, "statusCategory != Done",
		FixJQLShellEscapes(`statusCategory \!= Done`))
}

func TestFixJQLShellEscapesMultiple(t *testing.T) {
	assert.Equal(t, "status != Done AND type != Sub-task",
		FixJQLShellEscapes(`status \!= Done AND type \!= Sub-task`))
}

func TestFixJQLShellEscapesCleanUnchanged(t *testing.T) {
	jql := "statusCategory != Done"
	assert.Equal(t, jql, FixJQLShellEscapes(jql))
}

func TestFixJQLShellEscapesEmpty(t *testing.T) {
	assert.Equal(t, "", FixJQLShellEscapes(""))
}

func TestJQLHasProjectIgnoresStrings(t *testing.T) {
	assert.True(t, JQLHasProject("project = FOO"))
	assert.False(t, JQLHasProject(`summary ~ "project = FOO"`))
	assert.False(t, JQLHasProject(`summary ~ 'project in (FOO)'`))
}

func TestJQLHasProjectIn(t *testing.T) {
	assert.True(t, JQLHasProject("project in (FOO, BAR)"))
}

func TestApplyDefaultJQLScopeAddsWhenMissing(t *testing.T) {
	jql := ApplyDefaultJQLScope(`status = "Open"`, "project = ABC")
	assert.Equal(t, `(project = ABC) AND (status = "Open")`, jql)
}

func TestApplyDefaultJQLScopeSkipsWhenPresent(t *testing.T) {
	jql := ApplyDefaultJQLScope(`project = XYZ AND status = "Open"`, "project = ABC")
	assert.Equal(t, `project = XYZ AND status = "Open"`, jql)
}

func TestApplyDefaultJQLScopeNoScope(t *testing.T) {
	jql := ApplyDefaultJQLScope(`status = "Open"`, "")
	assert.Equal(t, `status = "Open"`, jql)
}

func TestJQLValueEmpty(t *testing.T) {
	assert.Equal(t, `""`, JQLValue(""))
}

func TestJQLValueAlreadyQuoted(t *testing.T) {
	assert.Equal(t, `"In Progress"`, JQLValue(`"In Progress"`))
}

func TestJQLValueFunction(t *testing.T) {
	assert.Equal(t, "currentUser()", JQLValue("currentUser()"))
}

func TestJQLValueNegative(t *testing.T) {
	assert.Equal(t, "-1d", JQLValue("-1d"))
}

func TestJQLValueNeedsQuoting(t *testing.T) {
	assert.Equal(t, `"High"`, JQLValue("High"))
}

func TestJQLValueEscapesQuotes(t *testing.T) {
	assert.Equal(t, `"foo\"bar"`, JQLValue(`foo"bar`))
}

func TestStripJQLStrings(t *testing.T) {
	result := StripJQLStrings(`project = "FOO" AND type = 'Bug'`)
	assert.NotContains(t, result, "FOO")
	assert.NotContains(t, result, "Bug")
	assert.Contains(t, result, "project")
	assert.Contains(t, result, "type")
}

func TestStripJQLOrderBy(t *testing.T) {
	assert.Equal(t, "project = FOO", StripJQLOrderBy("project = FOO ORDER BY created"))
	assert.Equal(t, "project = FOO", StripJQLOrderBy("project = FOO order by created"))
	assert.Equal(t, "project = FOO", StripJQLOrderBy("project = FOO"))
	assert.Equal(t, "", StripJQLOrderBy(""))
}

func TestStripJQLOrderByIgnoresQuoted(t *testing.T) {
	jql := `summary ~ "order by" AND project = FOO ORDER BY created`
	result := StripJQLOrderBy(jql)
	assert.Contains(t, result, "summary")
	assert.Contains(t, result, "project = FOO")
}
