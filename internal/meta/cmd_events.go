package meta

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/events"
	"github.com/spf13/cobra"
)

// NewEventsCmd returns the "events" command group.
func NewEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Tail structured progress and error events",
	}
	cmd.AddCommand(newEventsTailCmd())
	return cmd
}

func newEventsTailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tail [stream-id]",
		Short: "Print an event stream as newline-delimited JSON",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			streamID := ""
			if len(args) > 0 {
				streamID = args[0]
			}
			latest, _ := cmd.Flags().GetBool("latest")
			if streamID == "" || latest {
				id, err := events.LatestStreamID()
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						return &cerrors.CojiraError{
							Code:     cerrors.FileNotFound,
							Message:  "No event streams are available yet.",
							ExitCode: 1,
						}
					}
					return err
				}
				streamID = id
			}

			follow, _ := cmd.Flags().GetBool("follow")
			interval, _ := cmd.Flags().GetDuration("poll-interval")
			path := events.FilePath(streamID)
			if err := copyFileTo(cmd.OutOrStdout(), path); err != nil {
				if os.IsNotExist(err) {
					return &cerrors.CojiraError{
						Code:     cerrors.FileNotFound,
						Message:  fmt.Sprintf("Event stream not found: %s", streamID),
						ExitCode: 1,
					}
				}
				return err
			}
			if !follow {
				return nil
			}
			return followEventFile(cmd, path, interval)
		},
	}
	cmd.Flags().Bool("latest", false, "Use the most recently updated event stream")
	cmd.Flags().Bool("follow", false, "Keep polling for new event lines")
	cmd.Flags().Duration("poll-interval", 500*time.Millisecond, "Polling interval when using --follow")
	return cmd
}

func copyFileTo(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func followEventFile(cmd *cobra.Command, path string, interval time.Duration) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return err
			}
			if info.Size() < offset {
				offset = 0
			}
			if info.Size() == offset {
				continue
			}
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				return err
			}
			n, err := io.Copy(cmd.OutOrStdout(), f)
			if err != nil {
				return err
			}
			offset += n
		}
	}
}
