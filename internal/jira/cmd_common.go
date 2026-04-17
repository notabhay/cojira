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

// clientFromCmd creates a Jira Client from the cobra command's environment.
func clientFromCmd(cmd *cobra.Command) (*Client, error) {
	baseURL := strings.TrimSpace(os.Getenv("JIRA_BASE_URL"))
	token := strings.TrimSpace(os.Getenv("JIRA_API_TOKEN"))
	email := strings.TrimSpace(os.Getenv("JIRA_EMAIL"))
	if dotenv.IsPlaceholder(email, "email") {
		email = ""
	}
	apiVersion := strings.TrimSpace(os.Getenv("JIRA_API_VERSION"))
	if apiVersion == "" {
		apiVersion = DefaultAPIVersion
	}
	authMode := strings.TrimSpace(os.Getenv("JIRA_AUTH_MODE"))
	apiBaseURL := ""
	if strings.EqualFold(authMode, "oauth2") {
		resolved, err := oauth.ResolveAtlassianOAuth2(cmd.Context(), "jira", baseURL, "JIRA")
		if err != nil {
			return nil, err
		}
		token = resolved.AccessToken
		email = ""
		apiBaseURL = resolved.APIBaseURL
	}
	verifySSLStr := strings.TrimSpace(os.Getenv("JIRA_VERIFY_SSL"))
	verifySSL := true
	if verifySSLStr != "" {
		switch strings.ToLower(verifySSLStr) {
		case "false", "0", "no":
			verifySSL = false
		}
	}
	userAgent := strings.TrimSpace(os.Getenv("JIRA_USER_AGENT"))
	if userAgent == "" {
		userAgent = defaultUserAgent()
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
