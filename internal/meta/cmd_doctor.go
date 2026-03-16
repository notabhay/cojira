package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/confluence"
	"github.com/notabhay/cojira/internal/dotenv"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/httpclient"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// CheckResult holds the outcome of a single doctor check.
type CheckResult struct {
	OK      bool
	Name    string
	Details map[string]any
	Warning *string
	Error   map[string]any
}

// NewDoctorCmd returns the "cojira doctor" command.
func NewDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "doctor",
		Short:         "Pre-flight checks for Jira and Confluence configuration and connectivity",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runDoctor,
	}
	cli.AddHTTPRetryFlags(cmd)
	cli.AddOutputFlags(cmd, false)
	cmd.Flags().Bool("fix", false, "Attempt to fix missing env vars by writing a credentials file")
	cmd.Flags().Bool("interactive", false, "Allow prompts for --fix")
	return cmd
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	loadResult := dotenv.LoadIfPresent(dotenv.DefaultSearchPaths())
	cli.NormalizeOutputMode(cmd)
	jsonOut := cli.IsJSON(cmd)

	fix, _ := cmd.Flags().GetBool("fix")
	interactive, _ := cmd.Flags().GetBool("interactive")

	var fixResult map[string]any

	if fix {
		if !interactive {
			if jsonOut {
				errObj, _ := output.ErrorObj(cerrors.OpFailed,
					"--fix requires --interactive to allow prompts.", "", "", nil)
				ec := 3
				env := output.BuildEnvelope(
					false, "cojira", "doctor",
					map[string]any{}, nil,
					nil, []any{errObj}, "", "", "", &ec,
				)
				_ = output.PrintJSON(env)
				return &exitError{Code: 3}
			}
			fmt.Fprintln(os.Stderr, "Error: --fix requires --interactive to allow prompts.")
			return &exitError{Code: 3}
		}
		// TTY check: in Go we just check if stdin is a terminal.
		if !output.IsTTY(int(os.Stdin.Fd())) || !output.IsTTY(int(os.Stdout.Fd())) {
			if jsonOut {
				errObj, _ := output.ErrorObj(cerrors.OpFailed,
					"--fix requires a TTY.", "", "", nil)
				ec := 3
				env := output.BuildEnvelope(
					false, "cojira", "doctor",
					map[string]any{}, nil,
					nil, []any{errObj}, "", "", "", &ec,
				)
				_ = output.PrintJSON(env)
				return &exitError{Code: 3}
			}
			fmt.Fprintln(os.Stderr, "Error: --fix requires a TTY (interactive terminal).")
			return &exitError{Code: 3}
		}

		fixResult = runFix(jsonOut)
	}

	rc := cli.BuildRetryConfig(cmd)
	results := runDoctorChecks(rc)
	ok := true
	for _, r := range results {
		if !r.OK {
			ok = false
			break
		}
	}

	if jsonOut {
		var checksOut []map[string]any
		var errs []any
		var warns []any
		for _, r := range results {
			checksOut = append(checksOut, map[string]any{
				"name":    r.Name,
				"ok":      r.OK,
				"details": r.Details,
				"warning": r.Warning,
				"error":   r.Error,
			})
			if r.Error != nil {
				errs = append(errs, r.Error)
			}
			if r.Warning != nil {
				warns = append(warns, *r.Warning)
			}
		}
		result := map[string]any{
			"checks":      checksOut,
			"fix":         fixResult,
			"env_loading": loadResult,
			"env_sources": envSourcesReport(),
		}
		env := output.BuildEnvelope(
			ok, "cojira", "doctor",
			map[string]any{}, result,
			warns, errs, "", "", "", nil,
		)
		_ = output.PrintJSON(env)
		if !ok {
			return &exitError{Code: 1}
		}
		return nil
	}

	for _, r := range results {
		if r.OK {
			baseURL, _ := r.Details["base_url"].(string)
			user := ""
			if u, ok := r.Details["user"].(map[string]any); ok {
				user, _ = u["displayName"].(string)
			}
			source, _ := r.Details["credential_source"].(string)
			msg := r.Name + ": OK"
			var extras []string
			if baseURL != "" {
				extras = append(extras, "base_url="+baseURL)
			}
			if user != "" {
				extras = append(extras, "user="+user)
			}
			if source != "" {
				extras = append(extras, "source="+source)
			}
			if len(extras) > 0 {
				msg += " (" + strings.Join(extras, ", ") + ")"
			}
			receipt := output.Receipt{OK: true, Message: msg}
			fmt.Println(receipt.Format())
		} else {
			errMsg := "unknown error"
			if r.Error != nil {
				if m, ok := r.Error["message"].(string); ok {
					errMsg = m
				}
			}
			receipt := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %s", r.Name, errMsg)}
			fmt.Fprintln(os.Stderr, receipt.Format())
			if r.Error != nil {
				if hint, ok := r.Error["hint"].(string); ok && hint != "" {
					fmt.Fprintf(os.Stderr, "  hint: %s\n", hint)
				}
			}
		}
	}

	if !ok {
		return &exitError{Code: 1}
	}
	return nil
}

// runFix handles the --fix --interactive flow. It prompts for missing env vars
// and appends them to .env. Returns a result dict for JSON output.
func runFix(jsonOut bool) map[string]any {
	required := []string{
		"CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN",
		"JIRA_BASE_URL", "JIRA_API_TOKEN",
	}

	envPath := filepath.Join(".", ".env")
	if cred := dotenv.CredentialsPath(); cred != "" {
		envPath = cred
	}
	for _, p := range dotenv.DefaultSearchPaths() {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			envPath = p
			break
		}
	}

	var existingContent string
	if data, err := os.ReadFile(envPath); err == nil {
		existingContent = string(data)
	}
	existing := dotenv.ParseLines(existingContent)

	var missing []string
	for _, k := range required {
		if os.Getenv(k) == "" {
			if _, ok := existing[k]; !ok {
				missing = append(missing, k)
			}
		}
	}

	if len(missing) == 0 {
		if !jsonOut {
			fmt.Println("No missing required env vars detected.")
		}
		return map[string]any{"requested": missing, "written": []string{}, "path": envPath}
	}

	if !jsonOut {
		fmt.Println("cojira doctor --fix: fill missing values (tokens are hidden).")
	}

	values := promptMissingEnv(missing)
	written, writeErr := appendEnvValues(envPath, values, existing)

	for _, key := range written {
		_ = os.Setenv(key, values[key])
	}

	if writeErr != nil {
		if !jsonOut {
			fmt.Fprintf(os.Stderr, "Could not write credentials file %s: %v\n", envPath, writeErr)
		}
		return map[string]any{"requested": missing, "written": written, "path": envPath, "error": writeErr.Error()}
	}
	if len(written) > 0 && !jsonOut {
		fmt.Printf("Wrote %d value(s) to %s\n", len(written), envPath)
	} else if !jsonOut {
		fmt.Println("No values written.")
	}

	return map[string]any{"requested": missing, "written": written, "path": envPath}
}

// promptMissingEnv prompts the user for values of missing env vars.
// For vars containing "TOKEN", input is read without echoing.
var promptMissingEnv = func(missing []string) map[string]string {
	values := make(map[string]string)
	for _, key := range missing {
		if strings.Contains(key, "TOKEN") {
			fmt.Printf("%s (input hidden): ", key)
			tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println() // newline after hidden input
			if err != nil {
				continue
			}
			value := strings.TrimSpace(string(tokenBytes))
			if value != "" {
				values[key] = value
			}
		} else {
			fmt.Printf("%s: ", key)
			var value string
			_, _ = fmt.Scanln(&value)
			value = strings.TrimSpace(value)
			if value != "" {
				values[key] = value
			}
		}
	}
	return values
}

// appendEnvValues appends key=value pairs to the .env file for keys not already present.
func appendEnvValues(path string, values map[string]string, existing map[string]string) ([]string, error) {
	var toAdd []string
	for k, v := range values {
		if _, exists := existing[k]; !exists && v != "" {
			toAdd = append(toAdd, k)
		}
	}
	if len(toAdd) == 0 {
		return nil, nil
	}

	var existingContent string
	if data, err := os.ReadFile(path); err == nil {
		existingContent = string(data)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	if existingContent != "" && !strings.HasSuffix(existingContent, "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return nil, err
		}
	}
	for _, key := range toAdd {
		escaped := strings.ReplaceAll(values[key], `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		if _, err := fmt.Fprintf(f, "%s=\"%s\"\n", key, escaped); err != nil {
			return nil, err
		}
	}
	return toAdd, nil
}

// runDoctorChecks runs connectivity checks for Jira and Confluence.
func runDoctorChecks(rc cli.RetryConfig) []CheckResult {
	return []CheckResult{
		checkJira(rc),
		checkConfluence(rc),
	}
}

// toBool converts a string env var to boolean (matches Python's _to_bool).
func toBool(s string, def bool) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return def
	}
}

func checkJira(rc cli.RetryConfig) CheckResult {
	baseURL := strings.TrimSpace(os.Getenv("JIRA_BASE_URL"))
	token := strings.TrimSpace(os.Getenv("JIRA_API_TOKEN"))
	email := strings.TrimSpace(os.Getenv("JIRA_EMAIL"))
	if dotenv.IsPlaceholder(email, "email") {
		email = ""
	}
	apiVersion := strings.TrimSpace(os.Getenv("JIRA_API_VERSION"))
	if apiVersion == "" {
		apiVersion = "2"
	}
	authMode := strings.TrimSpace(os.Getenv("JIRA_AUTH_MODE"))
	verifySSL := toBool(os.Getenv("JIRA_VERIFY_SSL"), true)
	userAgent := strings.TrimSpace(os.Getenv("JIRA_USER_AGENT"))
	if userAgent == "" {
		userAgent = "cojira/0.1"
	}

	if baseURL == "" || token == "" {
		var missing []string
		if baseURL == "" {
			missing = append(missing, "JIRA_BASE_URL")
		}
		if token == "" {
			missing = append(missing, "JIRA_API_TOKEN")
		}
		errObj, _ := output.ErrorObj(cerrors.ConfigMissingEnv,
			fmt.Sprintf("Missing required env var(s): %s", strings.Join(missing, ", ")),
			cerrors.HintSetup(), "", nil)
		return CheckResult{
			OK:   false,
			Name: "jira",
			Details: mergeDetails(
				map[string]any{"configured": false, "missing_env": missing},
				toolCredentialDetails("JIRA_BASE_URL", "JIRA_API_TOKEN"),
			),
			Error: errObj,
		}
	}

	client, err := jira.NewClient(jira.ClientConfig{
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
	if err != nil {
		errObj, _ := output.ErrorObj(cerrors.ConfigInvalid, err.Error(),
			cerrors.HintSetup(), "", nil)
		return CheckResult{
			OK:   false,
			Name: "jira",
			Details: mergeDetails(
				map[string]any{"configured": true, "base_url": baseURL},
				toolCredentialDetails("JIRA_BASE_URL", "JIRA_API_TOKEN"),
			),
			Error: errObj,
		}
	}

	me, err := client.GetMyself()
	if err != nil {
		return jiraErrorResult(err, baseURL)
	}

	fields, err := client.ListFields()
	if err != nil {
		return jiraErrorResult(err, baseURL)
	}

	return CheckResult{
		OK:   true,
		Name: "jira",
		Details: mergeDetails(
			map[string]any{
				"configured": true,
				"base_url":   baseURL,
				"user": map[string]any{
					"displayName":  me["displayName"],
					"accountId":    me["accountId"],
					"emailAddress": me["emailAddress"],
				},
				"field_count": len(fields),
			},
			toolCredentialDetails("JIRA_BASE_URL", "JIRA_API_TOKEN"),
		),
	}
}

func jiraErrorResult(err error, baseURL string) CheckResult {
	msg := err.Error()
	code := cerrors.HTTPError
	hint := ""
	if strings.Contains(msg, "404") {
		code = cerrors.HTTP404
		hint = cerrors.HintBaseURL()
	} else if strings.Contains(msg, "401") || strings.Contains(msg, "403") {
		hint = cerrors.HintPermission()
		if os.Getenv("JIRA_EMAIL") != "" {
			hint += " " + cerrors.HintAuthMode()
		}
	}
	if isTimeoutError(err) {
		code = cerrors.Timeout
		hint = cerrors.HintTimeout(nil)
	}
	errObj, _ := output.ErrorObj(code, msg, hint, "", nil)
	return CheckResult{
		OK:   false,
		Name: "jira",
		Details: mergeDetails(
			map[string]any{"configured": true, "base_url": baseURL},
			toolCredentialDetails("JIRA_BASE_URL", "JIRA_API_TOKEN"),
		),
		Error: errObj,
	}
}

func checkConfluence(rc cli.RetryConfig) CheckResult {
	baseURL := strings.TrimSpace(os.Getenv("CONFLUENCE_BASE_URL"))
	token := strings.TrimSpace(os.Getenv("CONFLUENCE_API_TOKEN"))

	if baseURL == "" || token == "" {
		var missing []string
		if baseURL == "" {
			missing = append(missing, "CONFLUENCE_BASE_URL")
		}
		if token == "" {
			missing = append(missing, "CONFLUENCE_API_TOKEN")
		}
		errObj, _ := output.ErrorObj(cerrors.ConfigMissingEnv,
			fmt.Sprintf("Missing required env var(s): %s", strings.Join(missing, ", ")),
			cerrors.HintSetup(), "", nil)
		return CheckResult{
			OK:   false,
			Name: "confluence",
			Details: mergeDetails(
				map[string]any{"configured": false, "missing_env": missing},
				toolCredentialDetails("CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN"),
			),
			Error: errObj,
		}
	}

	client, err := confluence.NewClient(confluence.ClientConfig{
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
	if err != nil {
		errObj, _ := output.ErrorObj(cerrors.ConfigInvalid, err.Error(),
			cerrors.HintSetup(), "", nil)
		return CheckResult{
			OK:   false,
			Name: "confluence",
			Details: mergeDetails(
				map[string]any{"configured": true, "base_url": baseURL},
				toolCredentialDetails("CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN"),
			),
			Error: errObj,
		}
	}

	me, err := client.GetCurrentUser()
	if err != nil {
		return confluenceErrorResult(err, baseURL)
	}

	spaces, err := client.ListSpaces(1, 0)
	if err != nil {
		return confluenceErrorResult(err, baseURL)
	}

	var spaceSampleCount int
	if results, ok := spaces["results"].([]any); ok {
		spaceSampleCount = len(results)
	}

	return CheckResult{
		OK:   true,
		Name: "confluence",
		Details: mergeDetails(
			map[string]any{
				"configured": true,
				"base_url":   baseURL,
				"user": map[string]any{
					"displayName": me["displayName"],
					"accountId":   me["accountId"],
				},
				"space_sample_count": spaceSampleCount,
			},
			toolCredentialDetails("CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN"),
		),
	}
}

func confluenceErrorResult(err error, baseURL string) CheckResult {
	msg := err.Error()
	code := cerrors.HTTPError
	hint := ""
	if strings.Contains(msg, "404") {
		code = cerrors.HTTP404
		hint = cerrors.HintBaseURL()
	}
	if strings.Contains(msg, "401") || strings.Contains(msg, "403") {
		hint = cerrors.HintPermission()
	}
	if isTimeoutError(err) {
		code = cerrors.Timeout
		hint = cerrors.HintTimeout(nil)
	}
	errObj, _ := output.ErrorObj(code, msg, hint, "", nil)
	return CheckResult{
		OK:   false,
		Name: "confluence",
		Details: mergeDetails(
			map[string]any{"configured": true, "base_url": baseURL},
			toolCredentialDetails("CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN"),
		),
		Error: errObj,
	}
}

func envSourcesReport() map[string]map[string]any {
	return dotenv.Provenance([]string{
		"CONFLUENCE_BASE_URL",
		"CONFLUENCE_API_TOKEN",
		"JIRA_BASE_URL",
		"JIRA_API_TOKEN",
		"JIRA_EMAIL",
	})
}

func toolCredentialDetails(keys ...string) map[string]any {
	return map[string]any{
		"credential_source":  credentialSourceSummary(keys...),
		"credential_sources": dotenv.Provenance(keys),
	}
}

func credentialSourceSummary(keys ...string) string {
	sources := map[string]bool{}
	for _, key := range keys {
		source := dotenv.SourceForKey(key)
		if source == "" {
			continue
		}
		sources[source] = true
	}
	switch len(sources) {
	case 0:
		return ""
	case 1:
		for source := range sources {
			return source
		}
	}
	return "mixed"
}

func mergeDetails(parts ...map[string]any) map[string]any {
	merged := map[string]any{}
	for _, part := range parts {
		for key, value := range part {
			merged[key] = value
		}
	}
	return merged
}

// isTimeoutError checks if an error is a timeout.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	type timeoutErr interface {
		Timeout() bool
	}
	if te, ok := err.(timeoutErr); ok {
		return te.Timeout()
	}
	return strings.Contains(strings.ToLower(err.Error()), "timeout")
}
