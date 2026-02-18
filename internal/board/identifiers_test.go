package board

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveBoardIdentifierNumeric(t *testing.T) {
	assert.Equal(t, "45434", ResolveBoardIdentifier("45434"))
}

func TestResolveBoardIdentifierRapidViewURL(t *testing.T) {
	url := "https://jira.rakuten-it.com/jira/secure/RapidView.jspa?rapidView=45434&tab=swimlanes"
	assert.Equal(t, "45434", ResolveBoardIdentifier(url))
}

func TestResolveBoardIdentifierBoardsURL(t *testing.T) {
	url := "https://jira.example.com/jira/software/c/projects/PROJ/boards/1234"
	assert.Equal(t, "1234", ResolveBoardIdentifier(url))
}

func TestResolveBoardIdentifierQueryString(t *testing.T) {
	assert.Equal(t, "999", ResolveBoardIdentifier("rapidView=999"))
}

func TestResolveBoardIdentifierEmpty(t *testing.T) {
	assert.Equal(t, "", ResolveBoardIdentifier(""))
}

func TestResolveBoardIdentifierNonNumeric(t *testing.T) {
	assert.Equal(t, "myboard", ResolveBoardIdentifier("myboard"))
}
