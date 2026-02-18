package confluence

import (
	"encoding/base64"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePageGetter implements PageGetter for tests.
type fakePageGetter struct {
	pages map[[2]string]int // [space, title] -> pageID
}

func (f *fakePageGetter) GetPageByTitle(space, title string) (map[string]any, error) {
	id, ok := f.pages[[2]string{space, title}]
	if !ok {
		return nil, nil
	}
	return map[string]any{"id": id}, nil
}

func encodeTiny(pageID int64) string {
	size := 1
	for v := pageID; v >= 256; v >>= 8 {
		size++
	}
	buf := make([]byte, size)
	binary.LittleEndian.PutUint64(append(buf[:0], make([]byte, 8)...), uint64(pageID))
	// Trim to minimal size.
	buf = buf[:size]
	for i := 0; i < size; i++ {
		buf[i] = byte((uint64(pageID) >> (8 * i)) & 0xFF)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(buf)
}

func TestTinyCodeRoundtrip(t *testing.T) {
	pageID := int64(6219269629)
	code := encodeTiny(pageID)
	got, err := TinyCodeToPageID(code)
	require.NoError(t, err)
	assert.Equal(t, pageID, got)
}

func TestResolvePageIDNumericAndURLs(t *testing.T) {
	client := &fakePageGetter{
		pages: map[[2]string]int{{"CAIS", "My Page"}: 12345},
	}

	id, err := ResolvePageID(client, "123", "")
	require.NoError(t, err)
	assert.Equal(t, "123", id)

	id, err = ResolvePageID(client, "https://example/wiki/pages/viewpage.action?pageId=456", "")
	require.NoError(t, err)
	assert.Equal(t, "456", id)

	id, err = ResolvePageID(client, "https://example/wiki/pages/789/Whatever", "")
	require.NoError(t, err)
	assert.Equal(t, "789", id)
}

func TestResolvePageIDDisplayURLAndSpaceTitle(t *testing.T) {
	client := &fakePageGetter{
		pages: map[[2]string]int{{"CAIS", "My Page"}: 12345},
	}

	id, err := ResolvePageID(client, "CAIS:My Page", "")
	require.NoError(t, err)
	assert.Equal(t, "12345", id)

	id, err = ResolvePageID(client, `CAIS:"My Page"`, "")
	require.NoError(t, err)
	assert.Equal(t, "12345", id)

	id, err = ResolvePageID(client, "https://example/wiki/display/CAIS/My+Page", "")
	require.NoError(t, err)
	assert.Equal(t, "12345", id)

	id, err = ResolvePageID(client, "https://example/wiki/pages/viewpage.action?spaceKey=CAIS&title=My%20Page", "")
	require.NoError(t, err)
	assert.Equal(t, "12345", id)
}

func TestResolvePageIDUnrecognizedURL(t *testing.T) {
	client := &fakePageGetter{pages: map[[2]string]int{}}
	_, err := ResolvePageID(client, "https://example/wiki/unknown/path", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognized Confluence URL")
}

func TestResolvePageIDDefault(t *testing.T) {
	client := &fakePageGetter{pages: map[[2]string]int{}}
	id, err := ResolvePageID(client, "default", "99999")
	require.NoError(t, err)
	assert.Equal(t, "99999", id)

	id, err = ResolvePageID(client, "root", "88888")
	require.NoError(t, err)
	assert.Equal(t, "88888", id)
}
