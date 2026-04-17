package confluence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func capturePagePropsJSON(t *testing.T, run func() error) map[string]any {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	err = run()
	require.NoError(t, err)
	require.NoError(t, w.Close())

	var payload map[string]any
	require.NoError(t, json.NewDecoder(r).Decode(&payload))
	return payload
}

func TestPagePropertiesListJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/pages/12345/properties":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "1", "key": "foo"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")
	t.Setenv("CONFLUENCE_API_VERSION", "2")

	cmd := NewPagePropertiesCmd()
	cmd.SetArgs([]string{"list", "12345", "--output-mode", "json"})
	payload := capturePagePropsJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
}

func TestExtractPagePropertiesMacros(t *testing.T) {
	input := `
<ac:structured-macro ac:name="details">
  <ac:parameter ac:name="label">ops</ac:parameter>
  <ac:rich-text-body>
    <table>
      <tbody>
        <tr><th>Owner</th><td>Platform</td></tr>
        <tr><th>Status</th><td>Ready</td></tr>
      </tbody>
    </table>
  </ac:rich-text-body>
</ac:structured-macro>`

	macros := extractPagePropertiesMacros(input, "ops")
	require.Len(t, macros, 1)
	assert.Equal(t, "ops", macros[0].Label)
	assert.Equal(t, "Platform", macros[0].Properties["Owner"])
	assert.Equal(t, "Ready", macros[0].Properties["Status"])
}

func TestPagePropertiesReportMacroJSON(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			assert.Equal(t, "/rest/api/content/search", r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{"id": "12345", "title": "Ops Page"}},
			})
		case 2:
			assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "12345",
				"body": map[string]any{
					"storage": map[string]any{
						"value": `<ac:structured-macro ac:name="details"><ac:parameter ac:name="label">ops</ac:parameter><ac:rich-text-body><table><tbody><tr><th>Owner</th><td>Platform</td></tr></tbody></table></ac:rich-text-body></ac:structured-macro>`,
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewPagePropertiesCmd()
	cmd.SetArgs([]string{"report", "--cql", `space="TEAM"`, "--label", "ops", "--output-mode", "json"})
	payload := capturePagePropsJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["macro_report"])
}
