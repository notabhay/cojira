package jira

import (
	"os"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/dotenv"
	"github.com/notabhay/cojira/internal/httpclient"
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
		userAgent = DefaultUserAgent
	}

	rc := cli.BuildRetryConfig(cmd)

	return NewClient(ClientConfig{
		BaseURL:    baseURL,
		APIVersion: apiVersion,
		Email:      email,
		Token:      token,
		AuthMode:   authMode,
		VerifySSL:  verifySSL,
		UserAgent:  userAgent,
		Timeout:    time.Duration(rc.Timeout * float64(time.Second)),
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

// ClientFromCmd creates a Jira client from cobra command context for callers
// outside the jira package.
func ClientFromCmd(cmd *cobra.Command) (*Client, error) {
	return clientFromCmd(cmd)
}
