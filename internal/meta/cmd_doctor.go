package meta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/confluence"
	"github.com/notabhay/cojira/internal/credstore"
	"github.com/notabhay/cojira/internal/dotenv"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/httpclient"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/logging"
	"github.com/notabhay/cojira/internal/oauth"
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
	cmd.Flags().Bool("ci", false, "Emit CI-friendly status and reject interactive doctor flows")
	cmd.Flags().Bool("fix", false, "Attempt to fix missing env vars by writing a credentials file")
	cmd.Flags().Bool("interactive", false, "Allow prompts for --fix")
	return cmd
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	dotenv.LoadDefaultOnce()
	cli.NormalizeOutputMode(cmd)
	jsonOut := cli.IsJSON(cmd)
	profileOverrides, profileName, err := cli.ProfileEnvOverrides(cmd)
	if err != nil {
		return err
	}

	fix, _ := cmd.Flags().GetBool("fix")
	interactive, _ := cmd.Flags().GetBool("interactive")
	ciMode, _ := cmd.Flags().GetBool("ci")

	var fixResult map[string]any

	if ciMode && (fix || interactive) {
		if jsonOut {
			errObj, _ := output.ErrorObj(cerrors.OpFailed,
				"--ci cannot be combined with --fix or --interactive.", "", "", nil)
			ec := 2
			env := output.BuildEnvelope(
				false, "cojira", "doctor",
				map[string]any{"ci": true}, nil,
				nil, []any{errObj}, "", "", "", &ec,
			)
			_ = output.PrintJSON(env)
			return &exitError{Code: 2}
		}
		fmt.Fprintln(os.Stderr, "Error: --ci cannot be combined with --fix or --interactive.")
		return &exitError{Code: 2}
	}

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
	results := runDoctorChecks(rc, profileOverrides, profileName)
	ok := true
	warningCount := 0
	for _, r := range results {
		if !r.OK {
			ok = false
		}
		if r.Warning != nil {
			warningCount++
		}
	}
	summary := doctorSummary(results, ok, warningCount, ciMode)

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
		result := map[string]any{"checks": checksOut, "fix": fixResult, "summary": summary}
		env := output.BuildEnvelope(
			ok, "cojira", "doctor",
			map[string]any{"ci": ciMode}, result,
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
			msg := r.Name + ": OK"
			var extras []string
			if baseURL != "" {
				extras = append(extras, "base_url="+baseURL)
			}
			if user != "" {
				extras = append(extras, "user="+user)
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
	if ciMode {
		status := "PASS"
		if !ok {
			status = "FAIL"
		}
		fmt.Printf("doctor %s: %d ok, %d failed, %d warning\n", status, summary["ok_checks"], summary["failed_checks"], summary["warning_checks"])
	}

	if !ok {
		return &exitError{Code: 1}
	}
	return nil
}

func doctorSummary(results []CheckResult, ok bool, warningCount int, ciMode bool) map[string]any {
	failedChecks := 0
	okChecks := 0
	for _, result := range results {
		if result.OK {
			okChecks++
		} else {
			failedChecks++
		}
	}
	return map[string]any{
		"ready":            ok,
		"ci":               ciMode,
		"credential_store": credstore.EffectiveStoreName(),
		"total_checks":     len(results),
		"ok_checks":        okChecks,
		"failed_checks":    failedChecks,
		"warning_checks":   warningCount,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
	}
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
	written := appendEnvValues(envPath, values, existing)

	for _, key := range written {
		_ = os.Setenv(key, values[key])
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
func appendEnvValues(path string, values map[string]string, existing map[string]string) []string {
	var toAdd []string
	for k, v := range values {
		if _, exists := existing[k]; !exists && v != "" {
			toAdd = append(toAdd, k)
		}
	}
	if len(toAdd) == 0 {
		return nil
	}

	var existingContent string
	if data, err := os.ReadFile(path); err == nil {
		existingContent = string(data)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	if existingContent != "" && !strings.HasSuffix(existingContent, "\n") {
		_, _ = f.WriteString("\n")
	}
	for _, key := range toAdd {
		escaped := strings.ReplaceAll(values[key], `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		_, _ = fmt.Fprintf(f, "%s=\"%s\"\n", key, escaped)
	}
	return toAdd
}

// runDoctorChecks runs connectivity checks for Jira and Confluence.
func runDoctorChecks(rc cli.RetryConfig, profileOverrides map[string]string, profileName string) []CheckResult {
	return []CheckResult{
		checkCredentialStore(),
		checkJira(rc, profileOverrides, profileName),
		checkConfluence(rc, profileOverrides, profileName),
	}
}

func checkCredentialStore() CheckResult {
	store := credstore.ResolveStoreName()
	effective := credstore.EffectiveStoreName()
	plainExists, plainPath := credstore.HasPlainCredentials()
	keyringExists, keyringErr := credstore.KeyringStatus()
	details := map[string]any{
		"configured_store":  store,
		"effective_store":   effective,
		"plain_path":        plainPath,
		"plain_exists":      plainExists,
		"keyring_exists":    keyringExists,
		"keyring_available": credstore.KeyringAvailable(),
	}
	if keyringErr != nil {
		warning := keyringErr.Error()
		return CheckResult{OK: true, Name: "credentials", Details: details, Warning: &warning}
	}
	return CheckResult{OK: true, Name: "credentials", Details: details}
}

func envWithProfile(overrides map[string]string, key string) string {
	if value := strings.TrimSpace(overrides[key]); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(key))
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

func checkJira(rc cli.RetryConfig, profileOverrides map[string]string, profileName string) CheckResult {
	baseURL := envWithProfile(profileOverrides, "JIRA_BASE_URL")
	token := envWithProfile(profileOverrides, "JIRA_API_TOKEN")
	email := envWithProfile(profileOverrides, "JIRA_EMAIL")
	if dotenv.IsPlaceholder(email, "email") {
		email = ""
	}
	apiVersion := envWithProfile(profileOverrides, "JIRA_API_VERSION")
	if apiVersion == "" {
		apiVersion = "2"
	}
	authMode := envWithProfile(profileOverrides, "JIRA_AUTH_MODE")
	apiBaseURL := ""
	verifySSL := toBool(envWithProfile(profileOverrides, "JIRA_VERIFY_SSL"), true)
	userAgent := envWithProfile(profileOverrides, "JIRA_USER_AGENT")
	if userAgent == "" {
		userAgent = "cojira/0.1"
	}
	if profileName != "" {
		userAgent = userAgent + " profile/" + profileName
	}

	if strings.EqualFold(authMode, "oauth2") {
		resolved, err := oauth.ResolveAtlassianOAuth2WithOverrides(rc.Context, "jira", baseURL, "JIRA", profileOverrides)
		if err != nil {
			errObj, _ := output.ErrorObj(cerrors.ConfigMissingEnv, err.Error(), cerrors.HintSetup(), "", nil)
			return CheckResult{
				OK:      false,
				Name:    "jira",
				Details: map[string]any{"configured": false, "base_url": baseURL},
				Error:   errObj,
			}
		}
		token = resolved.AccessToken
		apiBaseURL = resolved.APIBaseURL
		email = ""
	}

	if baseURL == "" || token == "" {
		var missing []string
		if baseURL == "" {
			missing = append(missing, "JIRA_BASE_URL")
		}
		if token == "" {
			if strings.EqualFold(authMode, "oauth2") {
				missing = append(missing, "JIRA_OAUTH_ACCESS_TOKEN or JIRA_OAUTH_REFRESH_TOKEN")
			} else {
				missing = append(missing, "JIRA_API_TOKEN")
			}
		}
		errObj, _ := output.ErrorObj(cerrors.ConfigMissingEnv,
			fmt.Sprintf("Missing required env var(s): %s", strings.Join(missing, ", ")),
			cerrors.HintSetup(), "", nil)
		return CheckResult{
			OK:      false,
			Name:    "jira",
			Details: map[string]any{"configured": false, "missing_env": missing},
			Error:   errObj,
		}
	}

	client, err := jira.NewClient(jira.ClientConfig{
		BaseURL:    baseURL,
		APIBaseURL: apiBaseURL,
		APIVersion: apiVersion,
		Email:      email,
		Token:      token,
		AuthMode:   authMode,
		VerifySSL:  verifySSL,
		UserAgent:  userAgent,
		Context:    rc.Context,
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
		Debug: rc.Debug,
	})
	if err != nil {
		errObj, _ := output.ErrorObj(cerrors.ConfigInvalid, err.Error(),
			cerrors.HintSetup(), "", nil)
		return CheckResult{
			OK:      false,
			Name:    "jira",
			Details: map[string]any{"configured": true, "base_url": baseURL},
			Error:   errObj,
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
		Details: map[string]any{
			"configured": true,
			"base_url":   baseURL,
			"user": map[string]any{
				"displayName":  me["displayName"],
				"accountId":    me["accountId"],
				"emailAddress": me["emailAddress"],
			},
			"field_count": len(fields),
		},
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
		OK:      false,
		Name:    "jira",
		Details: map[string]any{"configured": true, "base_url": baseURL},
		Error:   errObj,
	}
}

func checkConfluence(rc cli.RetryConfig, profileOverrides map[string]string, profileName string) CheckResult {
	baseURL := envWithProfile(profileOverrides, "CONFLUENCE_BASE_URL")
	token := envWithProfile(profileOverrides, "CONFLUENCE_API_TOKEN")
	authMode := envWithProfile(profileOverrides, "CONFLUENCE_AUTH_MODE")
	apiVersion := envWithProfile(profileOverrides, "CONFLUENCE_API_VERSION")
	apiBaseURL := ""
	verifySSL := toBool(envWithProfile(profileOverrides, "CONFLUENCE_VERIFY_SSL"), true)
	userAgent := envWithProfile(profileOverrides, "CONFLUENCE_USER_AGENT")
	if userAgent == "" {
		userAgent = "cojira/0.1"
	}
	if profileName != "" {
		userAgent = userAgent + " profile/" + profileName
	}

	if strings.EqualFold(authMode, "oauth2") {
		resolved, err := oauth.ResolveAtlassianOAuth2WithOverrides(rc.Context, "confluence", baseURL, "CONFLUENCE", profileOverrides)
		if err != nil {
			errObj, _ := output.ErrorObj(cerrors.ConfigMissingEnv, err.Error(), cerrors.HintSetup(), "", nil)
			return CheckResult{
				OK:      false,
				Name:    "confluence",
				Details: map[string]any{"configured": false, "base_url": baseURL},
				Error:   errObj,
			}
		}
		token = resolved.AccessToken
		apiBaseURL = resolved.APIBaseURL
	}

	if baseURL == "" || token == "" {
		var missing []string
		if baseURL == "" {
			missing = append(missing, "CONFLUENCE_BASE_URL")
		}
		if token == "" {
			if strings.EqualFold(authMode, "oauth2") {
				missing = append(missing, "CONFLUENCE_OAUTH_ACCESS_TOKEN or CONFLUENCE_OAUTH_REFRESH_TOKEN")
			} else {
				missing = append(missing, "CONFLUENCE_API_TOKEN")
			}
		}
		errObj, _ := output.ErrorObj(cerrors.ConfigMissingEnv,
			fmt.Sprintf("Missing required env var(s): %s", strings.Join(missing, ", ")),
			cerrors.HintSetup(), "", nil)
		return CheckResult{
			OK:      false,
			Name:    "confluence",
			Details: map[string]any{"configured": false, "missing_env": missing},
			Error:   errObj,
		}
	}

	client, err := confluence.NewClient(confluence.ClientConfig{
		BaseURL:    baseURL,
		APIBaseURL: apiBaseURL,
		APIVersion: apiVersion,
		Token:      token,
		VerifySSL:  verifySSL,
		UserAgent:  userAgent,
		Context:    rc.Context,
		Logger:     logging.NewDebugLogger(rc.Debug, "confluence"),
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
		Debug: rc.Debug,
	})
	if err != nil {
		errObj, _ := output.ErrorObj(cerrors.ConfigInvalid, err.Error(),
			cerrors.HintSetup(), "", nil)
		return CheckResult{
			OK:      false,
			Name:    "confluence",
			Details: map[string]any{"configured": true, "base_url": baseURL},
			Error:   errObj,
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
		Details: map[string]any{
			"configured": true,
			"base_url":   baseURL,
			"user": map[string]any{
				"displayName": me["displayName"],
				"accountId":   me["accountId"],
			},
			"space_sample_count": spaceSampleCount,
		},
	}
}

func confluenceErrorResult(err error, baseURL string) CheckResult {
	msg := err.Error()
	code := cerrors.HTTPError
	hint := ""
	if strings.Contains(msg, "401") || strings.Contains(msg, "403") {
		hint = cerrors.HintPermission()
	}
	if isTimeoutError(err) {
		code = cerrors.Timeout
		hint = cerrors.HintTimeout(nil)
	}
	errObj, _ := output.ErrorObj(code, msg, hint, "", nil)
	return CheckResult{
		OK:      false,
		Name:    "confluence",
		Details: map[string]any{"configured": true, "base_url": baseURL},
		Error:   errObj,
	}
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
