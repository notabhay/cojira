package confluence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountPageTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/root/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "child-a", "title": "Child A"},
					{"id": "child-b", "title": "Child B"},
				},
			})
		case "/rest/api/content/child-a/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "grandchild", "title": "Grandchild"}}})
		case "/rest/api/content/child-b/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		case "/rest/api/content/grandchild/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	count := countPageTree(client, "root", newConfluenceSemaphore(3))
	assert.Equal(t, 4, count)
}

func TestFetchPageTreeRespectsDepthAndOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/root/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "child-a", "title": "Child A"},
					{"id": "child-b", "title": "Child B"},
				},
			})
		case "/rest/api/content/child-a/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "grandchild", "title": "Grandchild"}}})
		case "/rest/api/content/child-b/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	tree := fetchPageTree(client, "root", "Root", 0, 1, newConfluenceSemaphore(3))
	require.Len(t, tree.Children, 2)
	assert.Equal(t, "child-a", tree.Children[0].ID)
	assert.Equal(t, "child-b", tree.Children[1].ID)
	assert.Empty(t, tree.Children[0].Children)

	nodes := []map[string]any{{"id": tree.ID, "title": tree.Title, "parent_id": nil, "depth": 0}}
	flattenPageTree(tree, tree.ID, 1, &nodes)
	require.Len(t, nodes, 3)
	assert.Equal(t, "child-a", nodes[1]["id"])
	assert.Equal(t, 1, nodes[1]["depth"])
}
