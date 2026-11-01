package netease

import "testing"

func TestPlayableFollowsPrivilege(t *testing.T) {
	tracks := []Track{{ID: 1, Fee: 1}, {ID: 2, Fee: 8}, {ID: 3, Fee: 8}, {ID: 4, Fee: 1}}
	if tracks[0].Playable() || !tracks[1].Playable() {
		t.Fatal("without privileges, VIP songs do not play and free ones do")
	}
	withPrivileges(tracks, []Privilege{
		{ID: 1, Pl: 320000},           // VIP song, the listener subscribes
		{ID: 2, Pl: 0},                // nothing to play
		{ID: 3, St: -200, Pl: 128000}, // taken down
	})
	want := []bool{true, false, false, false}
	for i, tr := range tracks {
		if tr.Playable() != want[i] {
			t.Errorf("track %d: playable %v", tr.ID, tr.Playable())
		}
	}
}

// A song with an address of its own plays from there, whatever NetEase
// says of a song with its ID.
func TestPlayableAndURLFromPlayURL(t *testing.T) {
	tr := Track{ID: -1, Fee: 1, PlayURL: `C:\music\春日影.mp3`}
	if !tr.Playable() {
		t.Error("a third-party track does not play")
	}
	if tr.URL() != tr.PlayURL {
		t.Errorf("URL %q, want the address it plays from", tr.URL())
	}
}

// Only the image CDN's pictures are asked for at a size: a cover that is
// a file on this machine, or the art inside a music file, is served as it
// is.
func TestSized(t *testing.T) {
	for _, c := range []struct {
		pic  string
		n    int
		want string
	}{
		{"", 600, ""},
		{"http://p1.music.126.net/a.jpg", 600, "https://p1.music.126.net/a.jpg?param=600y600"},
		{"https://p1.music.126.net/a.jpg", 96, "https://p1.music.126.net/a.jpg?param=96y96"},
		{"//p1.music.126.net/a.jpg", 240, "//p1.music.126.net/a.jpg?param=240y240"},
		{`C:\music\春日影.mp3`, 600, `C:\music\春日影.mp3`},
		{"art:///music/春日影.flac", 600, "art:///music/春日影.flac"},
	} {
		if got := Sized(c.pic, c.n); got != c.want {
			t.Errorf("Sized(%q, %d) = %q, want %q", c.pic, c.n, got, c.want)
		}
	}
}

// What NetEase charges for is told apart from what is free: a song of the
// membership, one of an album that must be bought, and one whose higher
// bit rates alone are the membership`s. The mark is the song`s, not the
// listener`s: a subscriber still sees which songs are paid ones.
func TestPaidFollowsFee(t *testing.T) {
	for _, c := range []struct {
		fee  int
		want bool
	}{
		{0, false},  // free
		{1, true},   // the membership
		{4, true},   // an album that must be bought
		{8, true},   // the higher bit rates alone
		{16, false}, // something else
	} {
		if got := (&Track{Fee: c.fee}).Paid(); got != c.want {
			t.Errorf("fee %d: paid %v, want %v", c.fee, got, c.want)
		}
	}
}

// A song NetEase has withdrawn says so, whether or not the listener may
// play what is left of it.
func TestTakenDownFollowsPrivilege(t *testing.T) {
	if (&Track{}).TakenDown() {
		t.Error("a song with no privilege is taken down")
	}
	if !(&Track{Privilege: &Privilege{St: -200, Pl: 128000}}).TakenDown() {
		t.Error("a withdrawn song is not taken down")
	}
	if (&Track{Privilege: &Privilege{St: 0, Pl: 320000}}).TakenDown() {
		t.Error("a song on offer is taken down")
	}
}
