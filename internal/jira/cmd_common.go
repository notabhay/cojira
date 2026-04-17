package jira

import (
	"crypto/tls"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/dotenv"
	"github.com/notabhay/cojira/internal/httpclient"
	"github.com/notabhay/cojira/internal/logging"
	"github.com/notabhay/cojira/internal/oauth"
	"github.com/notabhay/cojira/internal/version"
	"github.com/spf13/cobra"
)

func envWithProfile(cmd *cobra.Command, overrides map[string]string, key string) string {
	if value := strings.TrimSpace(overrides[key]); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(key))
}

// clientFromCmd creates a Jira Client from the cobra command's environment.
func clientFromCmd(cmd *cobra.Command) (*Client, error) {
	overrides, profileName, err := cli.ProfileEnvOverrides(cmd)
	if err != nil {
		return nil, err
	}

	baseURL := envWithProfile(cmd, overrides, "JIRA_BASE_URL")
	token := envWithProfile(cmd, overrides, "JIRA_API_TOKEN")
	email := envWithProfile(cmd, overrides, "JIRA_EMAIL")
	if dotenv.IsPlaceholder(email, "email") {
		email = ""
	}
	apiVersion := envWithProfile(cmd, overrides, "JIRA_API_VERSION")
	if apiVersion == "" {
		apiVersion = DefaultAPIVersion
	}
	authMode := envWithProfile(cmd, overrides, "JIRA_AUTH_MODE")
	apiBaseURL := ""
	if strings.EqualFold(authMode, "oauth2") {
		resolved, err := oauth.ResolveAtlassianOAuth2WithOverrides(cmd.Context(), "jira", baseURL, "JIRA", overrides)
		if err != nil {
			return nil, err
		}
		token = resolved.AccessToken
		email = ""
		apiBaseURL = resolved.APIBaseURL
	}
	verifySSLStr := envWithProfile(cmd, overrides, "JIRA_VERIFY_SSL")
	verifySSL := true
	if verifySSLStr != "" {
		switch strings.ToLower(verifySSLStr) {
		case "false", "0", "no":
			verifySSL = false
		}
	}
	userAgent := envWithProfile(cmd, overrides, "JIRA_USER_AGENT")
	if userAgent == "" {
		userAgent = defaultUserAgent()
	}
	if profileName != "" {
		userAgent = userAgent + " profile/" + profileName
	}

	rc := cli.BuildRetryConfig(cmd)

	return NewClient(ClientConfig{
		BaseURL:    baseURL,
		APIVersion: apiVersion,
		Email:      email,
		Token:      token,
		AuthMode:   authMode,
		APIBaseURL: apiBaseURL,
		VerifySSL:  verifySSL,
		UserAgent:  userAgent,
		Context:    cmd.Context(),
		Logger:     logging.NewDebugLogger(rc.Debug, "jira"),
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

// ClientFromCmd creates a Jira client from cobra command context for callers
// outside the jira package.
func ClientFromCmd(cmd *cobra.Command) (*Client, error) {
	return clientFromCmd(cmd)
}

func buildHTTPClient(timeout time.Duration, verifySSL bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if !verifySSL {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func defaultUserAgent() string {
	return "cojira/" + version.Version
}
