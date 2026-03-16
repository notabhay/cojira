package meta

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/dotenv"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewInitCmd returns the "cojira init" command (interactive setup wizard).
func NewInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "init",
		Short:         "Interactive setup wizard for humans. Writes a local .env and runs doctor checks.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runInit,
	}
	cli.AddOutputFlags(cmd, false)
	cmd.Flags().String("path", ".env", "Path to write the env file")
	cmd.Flags().Bool("non-interactive", false, "Refuse prompts and print guidance (exit 3)")
	cmd.Flags().Bool("no-open-tokens", false, "Do not open token creation pages in a browser")
	return cmd
}

func runInit(cmd *cobra.Command, _ []string) error {
	cli.NormalizeOutputMode(cmd)
	jsonOut := cli.IsJSON(cmd)

	envPath, _ := cmd.Flags().GetString("path")
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")

	if nonInteractive {
		if jsonOut {
			errObj, _ := output.ErrorObj(cerrors.OpFailed,
				"`cojira init` is interactive.", "", "", nil)
			ec := 3
			env := output.BuildEnvelope(
				false, "cojira", "init",
				map[string]any{"path": envPath}, nil,
				nil, []any{errObj}, "", "", "", &ec,
			)
			_ = output.PrintJSON(env)
			return &exitError{Code: 3}
		}
		fmt.Fprintln(os.Stderr, "Error: `cojira init` is interactive. Use `cojira bootstrap` and edit .env manually.")
		return &exitError{Code: 3}
	}

	if !output.IsTTY(int(os.Stdin.Fd())) || !output.IsTTY(int(os.Stdout.Fd())) {
		if jsonOut {
			errObj, _ := output.ErrorObj(cerrors.OpFailed,
				"`cojira init` requires a TTY.", "", "", nil)
			ec := 3
			env := output.BuildEnvelope(
				false, "cojira", "init",
				map[string]any{"path": envPath}, nil,
				nil, []any{errObj}, "", "", "", &ec,
			)
			_ = output.PrintJSON(env)
			return &exitError{Code: 3}
		}
		fmt.Fprintln(os.Stderr, "Error: `cojira init` requires a TTY (interactive terminal).")
		fmt.Fprintln(os.Stderr, "Hint: Run `cojira bootstrap` and edit .env manually, then run `cojira doctor`.")
		return &exitError{Code: 3}
	}

	// Read existing .env if present.
	outPath := filepath.Clean(envPath)
	var existing map[string]string
	if data, err := os.ReadFile(outPath); err == nil {
		existing = dotenv.ParseLines(string(data))
	} else {
		existing = map[string]string{}
	}

	fmt.Println("cojira init: This will write credentials to a local .env file.")
	fmt.Println("Tokens are not echoed and will not be printed.")
	fmt.Println()

	// Confluence setup.
	confDefault := existing["CONFLUENCE_BASE_URL"]
	confluenceInput := promptWithDefault(
		"Confluence URL (paste a page URL from your browser)", confDefault)
	confBase := inferConfluenceBaseURL(confluenceInput)
	if confBase == "" {
		confBase = normalizeURLInput(confluenceInput)
	}
	if confBase != "" {
		fmt.Printf("Using Confluence base URL: %s\n", confBase)
	}

	fmt.Print("Paste your Confluence API token (input hidden): ")
	tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // newline after hidden input
	confluenceToken := strings.TrimSpace(string(tokenBytes))
	if err != nil {
		confluenceToken = ""
	}
	if confluenceToken == "" {
		confluenceToken = existing["CONFLUENCE_API_TOKEN"]
	}

	// Jira setup.
	jiraDefault := existing["JIRA_BASE_URL"]
	jiraInput := promptWithDefault(
		"Jira URL (paste an issue or board URL from your browser)", jiraDefault)
	jiraInput = normalizeURLInput(jiraInput)
	jiraBase := jira.InferBaseURL(jiraInput)
	if jiraBase == "" {
		jiraBase = jiraInput
	}
	if jiraBase != "" {
		probed := probeJiraURL(jiraBase, 10.0)
		if probed != "" && probed != jiraBase {
			fmt.Printf("Note: %s returned 404, but %s works.\n", jiraBase, probed)
			jiraBase = probed
		} else if probed == "" {
			fmt.Printf("Warning: Could not reach %s/rest/api/2/serverInfo -- the URL may need a context path (e.g. /jira).\n", jiraBase)
		}
		fmt.Printf("Using Jira base URL: %s\n", jiraBase)
	}

	fmt.Print("Paste your Jira API token (input hidden): ")
	jiraTokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println() // newline after hidden input
	jiraToken := strings.TrimSpace(string(jiraTokenBytes))
	if err != nil {
		jiraToken = ""
	}
	if jiraToken == "" {
		jiraToken = existing["JIRA_API_TOKEN"]
	}

	jiraEmailDefault := existing["JIRA_EMAIL"]
	jiraEmail := promptWithDefault("Jira email (optional)", jiraEmailDefault)
	if dotenv.IsPlaceholder(jiraEmail, "email") {
		jiraEmail = ""
	}

	// Write .env file.
	values := map[string]string{
		"CONFLUENCE_BASE_URL":  confBase,
		"CONFLUENCE_API_TOKEN": confluenceToken,
		"JIRA_BASE_URL":        jiraBase,
		"JIRA_API_TOKEN":       jiraToken,
	}
	if jiraEmail != "" {
		values["JIRA_EMAIL"] = jiraEmail
	}
	writeEnvFile(outPath, values)

	cojiraJSONPath := writeCojiraJSONStub(filepath.Dir(outPath), jiraInput, confluenceInput)

	if !jsonOut {
		fmt.Printf("\nWrote: %s\n", outPath)
		if cojiraJSONPath != "" {
			fmt.Printf("Wrote: %s\n", cojiraJSONPath)
		}
	}

	// Clear and reload env.
	for _, key := range []string{"CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN",
		"JIRA_BASE_URL", "JIRA_API_TOKEN", "JIRA_EMAIL"} {
		_ = os.Unsetenv(key)
	}
	dotenv.LoadIfPresent([]string{outPath})

	// Run doctor checks.
	results := runDoctorChecks(cli.RetryConfig{
		Timeout:        30.0,
		Retries:        2,
		RetryBaseDelay: 0.5,
		RetryMaxDelay:  4.0,
		Debug:          false,
	})
	allOK := true
	for _, r := range results {
		if !r.OK {
			allOK = false
			break
		}
	}

	if allOK {
		if jsonOut {
			env := output.BuildEnvelope(
				true, "cojira", "init",
				map[string]any{"path": envPath},
				map[string]any{
					"message": "Setup looks good.",
					"checks": func() []string {
						var names []string
						for _, r := range results {
							names = append(names, r.Name)
						}
						return names
					}(),
				},
				nil, nil, "", "", "", nil,
			)
			return output.PrintJSON(env)
		}
		fmt.Println("\nSetup looks good. Next: try `cojira jira whoami` or `cojira confluence info <page>`.")
		return nil
	}

	if jsonOut {
		var errs []any
		for _, r := range results {
			if r.Error != nil {
				errs = append(errs, r.Error)
			}
		}
		env := output.BuildEnvelope(
			false, "cojira", "init",
			map[string]any{"path": envPath},
			map[string]any{"message": "Some checks failed."},
			nil, errs, "", "", "", nil,
		)
		_ = output.PrintJSON(env)
		return &exitError{Code: 1}
	}

	fmt.Fprintln(os.Stderr, "\nSome checks failed:")
	for _, r := range results {
		if r.OK {
			continue
		}
		errMsg := "unknown error"
		if r.Error != nil {
			if m, ok := r.Error["message"].(string); ok {
				errMsg = m
			}
		}
		fmt.Fprintf(os.Stderr, "- %s: %s\n", r.Name, errMsg)
		if r.Error != nil {
			if hint, ok := r.Error["hint"].(string); ok && hint != "" {
				fmt.Fprintf(os.Stderr, "  hint: %s\n", hint)
			}
		}
	}
	return &exitError{Code: 1}
}

// promptWithDefault displays a prompt and returns user input, falling back to default.
var promptWithDefault = func(prompt string, defaultVal string) string {
	suffix := ""
	if defaultVal != "" {
		suffix = fmt.Sprintf(" [%s]", defaultVal)
	}
	fmt.Printf("%s%s: ", prompt, suffix)
	var value string
	_, _ = fmt.Scanln(&value)
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultVal
	}
	return value
}

// normalizeURLInput ensures a URL has a scheme.
func normalizeURLInput(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return "https://" + raw
}

// probeJiraURL tries to reach Jira's serverInfo endpoint; auto-detects
// context path if needed. Returns the working base URL or empty string.
var probeJiraURL = func(baseURL string, timeout float64) string {
	if baseURL == "" {
		return ""
	}

	candidates := []string{baseURL}
	parsed, err := url.Parse(baseURL)
	if err == nil && (parsed.Path == "" || parsed.Path == "/") {
		candidates = append(candidates, strings.TrimRight(baseURL, "/")+"/jira")
	}

	httpClient := &http.Client{Timeout: time.Duration(timeout * float64(time.Second))}
	for _, candidate := range candidates {
		url := strings.TrimRight(candidate, "/") + "/rest/api/2/serverInfo"
		resp, err := httpClient.Get(url)
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 500 && resp.StatusCode != 404 {
			return candidate
		}
	}
	return ""
}

// inferConfluenceBaseURL extracts the Confluence base URL from a page URL.
func inferConfluenceBaseURL(value string) string {
	raw := normalizeURLInput(value)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	path := parsed.Path
	if strings.Contains(path, "/wiki/") {
		basePath := strings.SplitN(path, "/wiki", 2)[0] + "/wiki"
		return strings.TrimRight(fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, basePath), "/")
	}
	for _, marker := range []string{"/display/", "/pages/", "/spaces/"} {
		if strings.Contains(path, marker) {
			basePath := strings.SplitN(path, marker, 2)[0]
			return strings.TrimRight(fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, basePath), "/")
		}
	}
	basePath := strings.TrimRight(path, "/")
	return strings.TrimRight(fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, basePath), "/")
}

// tokenURLForBase returns the token creation URL for a given base URL.
func tokenURLForBase(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	if strings.Contains(parsed.Host, "atlassian.net") {
		return "https://id.atlassian.com/manage-profile/security/api-tokens"
	}
	return strings.TrimRight(baseURL, "/") + "/plugins/servlet/personal-access-tokens"
}

// writeEnvFile writes a .env file with the given values.
func writeEnvFile(path string, values map[string]string) {
	lines := []string{
		"# Generated by `cojira init`",
		"# Never commit .env to version control.",
		"",
		"# Confluence",
		fmt.Sprintf("CONFLUENCE_BASE_URL=%s", values["CONFLUENCE_BASE_URL"]),
		fmt.Sprintf("CONFLUENCE_API_TOKEN=%s", values["CONFLUENCE_API_TOKEN"]),
		"",
		"# Jira",
		fmt.Sprintf("JIRA_BASE_URL=%s", values["JIRA_BASE_URL"]),
		fmt.Sprintf("JIRA_API_TOKEN=%s", values["JIRA_API_TOKEN"]),
	}
	if email, ok := values["JIRA_EMAIL"]; ok && email != "" {
		lines = append(lines, fmt.Sprintf("JIRA_EMAIL=%s", email))
	}
	lines = append(lines, "")

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
}

var browseProjectRe = regexp.MustCompile(`/browse/([A-Za-z][A-Za-z0-9_]+)-\d+`)

// writeCojiraJSONStub writes a minimal .cojira.json if one does not exist.
// Returns the path written, or empty string if skipped.
func writeCojiraJSONStub(directory string, jiraInput string, confluenceInput string) string {
	target := filepath.Join(directory, ".cojira.json")
	if _, err := os.Stat(target); err == nil {
		return "" // already exists
	}

	stub := map[string]any{}

	// Detect default project from JIRA_PROJECT env or pasted URL.
	jiraProject := strings.TrimSpace(os.Getenv("JIRA_PROJECT"))
	if jiraProject == "" && jiraInput != "" {
		if m := browseProjectRe.FindStringSubmatch(jiraInput); m != nil {
			jiraProject = m[1]
		}
	}

	jiraSection := map[string]any{}
	if jiraProject != "" {
		jiraSection["default_project"] = jiraProject
		jiraSection["default_jql_scope"] = "project = " + jiraProject
	}
	stub["jira"] = jiraSection

	// Detect default space from Confluence URL.
	var confSpace string
	if confluenceInput != "" {
		for _, marker := range []string{"/display/", "/spaces/"} {
			if strings.Contains(confluenceInput, marker) {
				after := strings.SplitN(confluenceInput, marker, 2)[1]
				candidate := strings.SplitN(after, "/", 2)[0]
				if candidate != "" && isAlphaNumDash(candidate) {
					confSpace = candidate
					break
				}
			}
		}
	}

	confSection := map[string]any{}
	if confSpace != "" {
		confSection["default_space"] = confSpace
	}
	stub["confluence"] = confSection
	stub["aliases"] = map[string]any{}

	data, _ := json.MarshalIndent(stub, "", "  ")
	_ = os.WriteFile(target, append(data, '\n'), 0o644)
	return target
}

// isAlphaNumDash returns true if s contains only alphanumeric chars and dashes.
func isAlphaNumDash(s string) bool {
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
