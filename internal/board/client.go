package board

import (
	"fmt"
	"net/http"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/spf13/cobra"
)

func resolveBoardClient(cmd *cobra.Command, clientFn func(cmd *cobra.Command) (*jira.Client, error)) (*jira.Client, error) {
	if clientFn == nil {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.Error,
			Message:     "Board command is missing Jira client initialization.",
			UserMessage: "This board command hit an internal setup error. Please update cojira and try again.",
			ExitCode:    1,
		}
	}

	client, err := clientFn(cmd)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.Error,
			Message:     "Board command returned a nil Jira client.",
			UserMessage: "This board command hit an internal setup error. Please update cojira and try again.",
			ExitCode:    1,
		}
	}

	return client, nil
}

func requireResponseBody(resp *http.Response, operation string) error {
	if resp != nil && resp.Body != nil {
		return nil
	}

	return &cerrors.CojiraError{
		Code:        cerrors.FetchFailed,
		Message:     fmt.Sprintf("%s returned an empty response body.", operation),
		UserMessage: "I couldn't retrieve that board configuration. Please try again.",
		ExitCode:    1,
	}
}
