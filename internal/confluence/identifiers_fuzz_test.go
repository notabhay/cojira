package confluence

import (
	"fmt"
	"strings"
	"testing"
)

type fuzzPageGetter struct{}

func (fuzzPageGetter) GetPageByTitle(space, title string) (map[string]any, error) {
	if strings.TrimSpace(space) == "" || strings.TrimSpace(title) == "" {
		return nil, nil
	}
	return map[string]any{"id": fmt.Sprintf("%d", len(space)+len(title))}, nil
}

func FuzzResolvePageID(f *testing.F) {
	seeds := []string{
		"12345",
		"default",
		"root",
		"https://example/wiki/pages/viewpage.action?pageId=456",
		"https://example/wiki/pages/789/Whatever",
		"https://example/wiki/display/CAIS/My+Page",
		`CAIS:"My Page"`,
		"APnAVAE",
		"https://example/wiki/unknown/path",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	client := fuzzPageGetter{}
	f.Fuzz(func(t *testing.T, input string) {
		resolved, err := ResolvePageID(client, input, "99999")
		if err != nil {
			return
		}
		if strings.TrimSpace(resolved) == "" {
			t.Fatalf("successful resolution returned empty id for %q", input)
		}
		if strings.ContainsRune(resolved, '\x00') {
			t.Fatalf("unexpected NUL in resolved page id: %q", resolved)
		}
	})
}

func FuzzTinyCodeToPageID(f *testing.F) {
	seeds := []string{"APnAVAE", "abc", "////", "bad==", ""}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		_, _ = TinyCodeToPageID(input)
	})
}
