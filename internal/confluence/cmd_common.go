package confluence

import (
	"os"
	"strings"
	"time"

	"github.com/cojira/cojira/internal/cli"
	"github.com/cojira/cojira/internal/config"
	"github.com/cojira/cojira/internal/httpclient"
	"github.com/spf13/cobra"
)

// clientFromCmd creates a Confluence Client from the cobra command's environment.
func clientFromCmd(cmd *cobra.Command) (*Client, error) {
	baseURL := strings.TrimSpace(os.Getenv("CONFLUENCE_BASE_URL"))
	// Allow --base-url flag to override env.
	if flagURL, _ := cmd.Flags().GetString("base-url"); flagURL != "" {
		baseURL = flagURL
	}
	token := strings.TrimSpace(os.Getenv("CONFLUENCE_API_TOKEN"))

	rc := cli.BuildRetryConfig(cmd)

	return NewClient(ClientConfig{
		BaseURL: baseURL,
		Token:   token,
		Timeout: time.Duration(rc.Timeout * float64(time.Second)),
		RetryConfig: httpclient.RetryConfig{
			Retries:           rc.Retries,
			BaseDelay:         time.Duration(rc.RetryBaseDelay * float64(time.Second)),
			MaxDelay:          time.Duration(rc.RetryMaxDelay * float64(time.Second)),
			MaxRetryAfter:     300 * time.Second,
			JitterRatio:       0.1,
			RespectRetryAfter: true,
			RetryExceptions:   true,
			RetryStatuses:     map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true},
		},
		Debug: rc.Debug,
	})
}

// loadProjectConfigData loads project config and returns the raw data map.
// Returns nil if no config exists.
func loadProjectConfigData() map[string]any {
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil || cfg == nil {
		return nil
	}
	return cfg.Data
}
