package confluence

import (
	"crypto/tls"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/config"
	"github.com/notabhay/cojira/internal/httpclient"
	"github.com/notabhay/cojira/internal/logging"
	"github.com/notabhay/cojira/internal/oauth"
	"github.com/notabhay/cojira/internal/version"
	"github.com/spf13/cobra"
)

func envWithProfile(overrides map[string]string, key string) string {
	if value := strings.TrimSpace(overrides[key]); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(key))
}

// clientFromCmd creates a Confluence Client from the cobra command's environment.
func clientFromCmd(cmd *cobra.Command) (*Client, error) {
	overrides, profileName, err := cli.ProfileEnvOverrides(cmd)
	if err != nil {
		return nil, err
	}

	baseURL := envWithProfile(overrides, "CONFLUENCE_BASE_URL")
	// Allow --base-url flag to override env.
	if flagURL, _ := cmd.Flags().GetString("base-url"); flagURL != "" {
		baseURL = flagURL
	}
	token := envWithProfile(overrides, "CONFLUENCE_API_TOKEN")
	authMode := envWithProfile(overrides, "CONFLUENCE_AUTH_MODE")
	apiVersion := envWithProfile(overrides, "CONFLUENCE_API_VERSION")
	apiBaseURL := ""
	if strings.EqualFold(authMode, "oauth2") {
		resolved, err := oauth.ResolveAtlassianOAuth2WithOverrides(cmd.Context(), "confluence", baseURL, "CONFLUENCE", overrides)
		if err != nil {
			return nil, err
		}
		token = resolved.AccessToken
		apiBaseURL = resolved.APIBaseURL
	}
	verifySSL := true
	verifySSLStr := envWithProfile(overrides, "CONFLUENCE_VERIFY_SSL")
	if verifySSLStr != "" {
		switch strings.ToLower(verifySSLStr) {
		case "false", "0", "no":
			verifySSL = false
		}
	}
	userAgent := envWithProfile(overrides, "CONFLUENCE_USER_AGENT")
	if userAgent == "" {
		userAgent = confluenceDefaultUserAgent()
	}
	if profileName != "" {
		userAgent = userAgent + " profile/" + profileName
	}

	rc := cli.BuildRetryConfig(cmd)

	return NewClient(ClientConfig{
		BaseURL:    baseURL,
		APIBaseURL: apiBaseURL,
		APIVersion: apiVersion,
		Token:      token,
		UserAgent:  userAgent,
		Context:    cmd.Context(),
		Logger:     logging.NewDebugLogger(rc.Debug, "confluence"),
		VerifySSL:  verifySSL,
		Timeout:    time.Duration(rc.Timeout * float64(time.Second)),
		RetryConfig: httpclient.RetryConfig{
			Context:           rc.Context,
			Retries:           rc.Retries,
			BaseDelay:         time.Duration(rc.RetryBaseDelay * float64(time.Second)),
			MaxDelay:          time.Duration(rc.RetryMaxDelay * float64(time.Second)),
			MaxRetryAfter:     300 * time.Second,
			JitterRatio:       0.1,
			RespectRetryAfter: true,
			RetryExceptions:   true,
			ClientRateLimit:   rc.ClientRateLimit,
			ClientBurst:       rc.ClientBurst,
			RetryStatuses:     map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true},
		},
		CacheConfig: httpclient.CacheConfig{
			Disabled: rc.NoCache,
			TTL:      rc.CacheTTL,
		},
		Debug: rc.Debug,
	})
}

func buildConfluenceHTTPClient(timeout time.Duration, verifySSL bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func confluenceDefaultUserAgent() string {
	return "cojira/" + version.Version
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
