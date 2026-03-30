package board

import (
	"testing"

	"github.com/notabhay/cojira/internal/jira"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwimlanesGetNilClientFnReturnsError(t *testing.T) {
	cmd := newSwimlanesGetCmd(nil)
	cmd.Flags().Bool("experimental", true, "")
	cmd.SetArgs([]string{"45434", "--output-mode", "json"})

	require.NotPanics(t, func() {
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing Jira client initialization")
	})
}

func TestDetailViewGetNilClientFnReturnsError(t *testing.T) {
	cmd := newDetailViewGetCmd(nil)
	cmd.Flags().Bool("experimental", true, "")
	cmd.SetArgs([]string{"45434", "--output-mode", "json"})

	require.NotPanics(t, func() {
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing Jira client initialization")
	})
}

func TestResolveBoardClientRejectsNilClient(t *testing.T) {
	client, err := resolveBoardClient(&cobra.Command{}, func(cmd *cobra.Command) (*jira.Client, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "nil Jira client")
}
