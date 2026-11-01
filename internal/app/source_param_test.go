package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"netease-music-mygo/internal/source"
)

// An address copied from a browser's own search box carries the words it
// was searched for. It is searched with all the same, in place of them:
// before this, pasting one left the field saying no source could search,
// because the address held no {keyword}.
func TestAddressWithSearchWordsIsSearchable(t *testing.T) {
	for _, c := range []struct {
		name string
		addr string
		want bool
	}{
		{"a name", "https://x/api.php?types=search&source=netease&name=海阔天空&count=30", true},
		{"keywords", "https://x/search?keywords=海阔天空", true},
		{"an ordinary endpoint", "https://x/playlist?id=42", false},
		{"a folder", "C:/music", false},
		{"an empty param", "https://x/search?name=", false},
	} {
		e := source.Entry{ID: "s", Kind: source.KindAPI, Value: c.addr, Enabled: true}
		if got := e.Searchable(); got != c.want {
			t.Errorf("%s: searchable %v, want %v", c.name, got, c.want)
		}
	}
}

// Searching such an address puts the words in place of the ones it held,
// and leaves the rest of the address as it was.
func TestSearchAddrReplacesTheWordsItHolds(t *testing.T) {
	e := source.Entry{ID: "s", Kind: source.KindAPI,
		Value: "https://x/api.php?types=search&source=netease&name=海阔天空&count=30", Enabled: true}
	got, ok := source.SearchAddr(e, "晴天")
	if !ok {
		t.Fatal("the address cannot be searched")
	}
	for _, want := range []string{"types=search", "source=netease", "count=30"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lost %q", got, want)
		}
	}
	if strings.Contains(got, "%E6%B5%B7%E9%98%94%E5%A4%A9%E7%A9%BA") {
		t.Errorf("%q still holds the song it was searched for", got)
	}
	if !strings.Contains(got, "name=%E6%99%B4%E5%A4%A9") {
		t.Errorf("%q does not name the song asked for", got)
	}
}

// What the page searches is what the interface is asked for: the pasted
// address goes out with the typed words in it.
func TestSourceSearchAsksAPastedAddress(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("name")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"name":"晴天","artist":"周杰伦","url":"https://cdn.test/a.mp3"}]`))
	}))
	defer srv.Close()

	entries := []source.Entry{{ID: "s", Name: "GD", Kind: source.KindAPI,
		Value: srv.URL + "/api.php?types=search&name=海阔天空&count=30", Enabled: true}}
	found, failed := source.Search(entries, "晴天")
	if len(failed) > 0 {
		t.Fatalf("search failed: %v", failed)
	}
	if asked != "晴天" {
		t.Errorf("the interface was asked for %q, want 晴天", asked)
	}
	if len(found) != 1 || found[0].Name != "晴天" {
		t.Errorf("found %+v", found)
	}
}
