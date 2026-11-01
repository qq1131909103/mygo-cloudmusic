package player

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/netease"
)

// playAll decodes the whole of a stream.
func playAll(t *testing.T, s stream) []float32 {
	t.Helper()
	var out []float32
	buf := make([]float32, 4096)
	for {
		n, err := s.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil || n == 0 {
			return out
		}
	}
}

// A track that names a file on this machine plays from it, read where it
// is: what it plays is what the file holds, and nothing of NetEase is
// asked for.
func TestOwnSourcePlaysLocalFile(t *testing.T) {
	path, err := filepath.Abs("testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Skipf("no MP3 fixture: %v", err)
	}
	tr := netease.Track{ID: -1, Name: "春日影", Duration: 5041, PlayURL: path}
	st, dl, err := ownSource(tr, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Level != sourceLevel || st.Type != "mp3" || st.Size != info.Size() || st.URL != path {
		t.Errorf("stream %+v", st)
	}
	s, err := openStream(dl, tr.Duration)
	if err != nil {
		t.Fatal(err)
	}
	got := playAll(t, s)
	if err := s.Close(); err != nil {
		t.Error(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := decodeAll(t, b)
	if len(got) < len(want)/2 {
		t.Fatalf("played %d samples of the file's %d", len(got), len(want))
	}
	if d := maxDiff(got, want); d > 1e-6 {
		t.Errorf("what it plays differs from the file by up to %v", d)
	}
}

// A track whose source streams it from an address is played from there,
// as it arrives.
func TestOwnSourceStreamsAddress(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.mp3")
	if err != nil {
		t.Skipf("no MP3 fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(b)
	}))
	defer srv.Close()

	tr := netease.Track{Name: "春日影", Duration: 5041, PlayURL: srv.URL + "/a.mp3?token=x"}
	st, dl, err := ownSource(tr, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Level != sourceLevel || st.Type != "mp3" || st.Size != 0 {
		t.Errorf("stream %+v", st)
	}
	s, err := openStream(dl, tr.Duration)
	if err != nil {
		t.Fatal(err)
	}
	got := playAll(t, s)
	defer s.Close()
	if want := decodeAll(t, b); maxDiff(got, want) > 1e-6 {
		t.Errorf("play %d samples differ from what the address serves", len(got))
	}
}

// What a third-party stream holds is read from its address, and one the
// address does not say is left for the decoder to find.
func TestStreamExt(t *testing.T) {
	for _, c := range []struct{ url, want string }{
		{"/music/春日影.mp3", "mp3"},
		{`C:\music\春日影.FLAC`, "flac"},
		{"http://127.0.0.1:8080/stream?token=1", ""},
		{"http://127.0.0.1:8080/a.mp3?token=1#x", "mp3"},
		{"/music/春日影.ogg", "ogg"},
		{"/music/春日影.wav", "wav"},
		{"/music/春日影.m4a", "m4a"},
		{"/music/春日影.wma", "wma"},
	} {
		if got := streamExt(c.url); got != c.want {
			t.Errorf("%q read as %q, want %q", c.url, got, c.want)
		}
	}
}
