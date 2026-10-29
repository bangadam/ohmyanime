package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseTitleLinkValid(t *testing.T) {
	valid := []struct {
		raw  string
		want TitleLink
	}{
		{"myanime://anime/anilist:16498", TitleLink{Kind: "anime", TitleID: "anilist:16498"}},
		{"myanime://manga/mangadex:f9c33607-9180-4ba6-b85c-e4b5faee7192", TitleLink{Kind: "manga", TitleID: "mangadex:f9c33607-9180-4ba6-b85c-e4b5faee7192"}},
		{"myanime://anime/short", TitleLink{Kind: "anime", TitleID: "short"}},
		{"MYANIME://ANIME/anilist:1", TitleLink{Kind: "anime", TitleID: "anilist:1"}},
	}
	for _, test := range valid {
		link, ok := ParseTitleLink(test.raw)
		if !ok {
			t.Errorf("ParseTitleLink(%q) rejected a valid link", test.raw)
			continue
		}
		if link != test.want {
			t.Errorf("ParseTitleLink(%q) = %+v, want %+v", test.raw, link, test.want)
		}
	}
}

func TestParseTitleLinkRejectsMalformed(t *testing.T) {
	invalid := []string{
		"", "myanime:", "myanime://", "myanime://anime", "myanime://anime/",
		"myanime://anime/a/b", "myanime://anime/a%2Fb", "myanime://anime/a//b",
		"myanime://novel/a", "myanime://animemode/a", "myanime://host:8080/a",
		"myanime://user@anime/a", "myanime://anime/a?episode=2",
		"myanime://anime/a#top", "myanime://anime/%2e%2e%2fetc",
		// Title IDs must be a bounded identifier, not arbitrary text.
		"myanime://anime/hello world", "myanime://anime/<script>",
		"myanime://anime/" + longID(maxTitleIDLen+1),
		// No credentials ever travel in a link.
		"myanime://anime:secret@a",
	}
	for _, raw := range invalid {
		if _, ok := ParseTitleLink(raw); ok {
			t.Errorf("ParseTitleLink(%q) accepted a malformed link", raw)
		}
	}
}

func TestParseTitleLinkRejectsExternalURLs(t *testing.T) {
	external := []string{
		"https://myanime.example/anime/anilist:1",
		"http://127.0.0.1:8080/manga/x",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"data:text/html,myanime://anime/a",
		"myanimelist://anime/a",
		"myanimemore://anime/a",
		"mygo://localhost",
		"//myanime.example/anime/a",
		"myanime:///anime/a",
	}
	for _, raw := range external {
		if _, ok := ParseTitleLink(raw); ok {
			t.Errorf("ParseTitleLink(%q) accepted an external URL", raw)
		}
	}
}

func TestParseTitleLinkMaxLengthAccepted(t *testing.T) {
	if _, ok := ParseTitleLink("myanime://anime/" + longID(maxTitleIDLen)); !ok {
		t.Error("ParseTitleLink rejected a title ID of exactly maxTitleIDLen")
	}
}

func longID(n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = 'a'
	}
	return string(buf)
}

func TestFirstTitleLink(t *testing.T) {
	link, ok := firstTitleLink([]string{"--flag", "https://elsewhere.example/x", "myanime://manga/mangadex:1"})
	if !ok || link != (TitleLink{Kind: "manga", TitleID: "mangadex:1"}) {
		t.Errorf("firstTitleLink = %+v, %v", link, ok)
	}
	if _, ok := firstTitleLink([]string{"--flag", "https://elsewhere.example/x"}); ok {
		t.Error("firstTitleLink accepted arguments without a title link")
	}
}

// testSocketPath returns a short, unique socket path: unix socket paths are
// limited to about 100 bytes, which t.TempDir() alone can exceed.
func testSocketPath(t *testing.T) string {
	t.Helper()
	// Hash like instanceLockPath: the test name alone can exceed the unix
	// socket path limit.
	sum := sha256.Sum256([]byte(t.Name()))
	path := filepath.Join(os.TempDir(), "myanime-test-"+hex.EncodeToString(sum[:6])+".sock")
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func TestInstanceLockSecondAcquireFailsAndSignals(t *testing.T) {
	path := testSocketPath(t)

	signaled := make(chan []string, 1)
	first, primary, err := TakeInstanceLock(path, nil, func(args []string) {
		select {
		case signaled <- args:
		default:
		}
	})
	if err != nil || !primary {
		t.Fatalf("first TakeInstanceLock = primary=%v, err=%v", primary, err)
	}
	defer func() {
		if err := first.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	second, primary2, err := TakeInstanceLock(path, []string{"myanime://anime/anilist:1"}, nil)
	if err != nil {
		t.Fatalf("second TakeInstanceLock error: %v", err)
	}
	if primary2 {
		t.Fatal("second TakeInstanceLock reported primary; a second database writer would result")
	}
	if second != nil {
		t.Fatal("second TakeInstanceLock returned a lock")
	}

	select {
	case args := <-signaled:
		want := []string{"myanime://anime/anilist:1"}
		if len(args) != 1 || args[0] != want[0] {
			t.Errorf("signaled args = %v, want %v", args, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the running instance was never signaled")
	}
}

func TestInstanceLockReleaseAllowsNextAcquire(t *testing.T) {
	path := testSocketPath(t)

	first, primary, err := TakeInstanceLock(path, nil, nil)
	if err != nil || !primary {
		t.Fatalf("first TakeInstanceLock = primary=%v, err=%v", primary, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, primary2, err := TakeInstanceLock(path, nil, nil)
	if err != nil || !primary2 {
		t.Fatalf("TakeInstanceLock after release = primary=%v, err=%v", primary2, err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestInstanceLockStaleSocketTakeover(t *testing.T) {
	path := testSocketPath(t)
	// A crashed instance leaves its socket behind with nobody listening.
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	listener.Close() // closed but not removed
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale socket: %v", err)
	}

	lock, primary, err := TakeInstanceLock(path, nil, nil)
	if err != nil || !primary {
		t.Fatalf("TakeInstanceLock over a stale socket = primary=%v, err=%v", primary, err)
	}
	defer lock.Close()

	// And the new owner answers signals.
	if _, primary2, err := TakeInstanceLock(path, nil, nil); err != nil || primary2 {
		t.Fatalf("second acquire after takeover = primary=%v, err=%v", primary2, err)
	}
}

func TestInstanceLockPathDistinctPerDataDir(t *testing.T) {
	a, b := instanceLockPath("/one"), instanceLockPath("/two")
	if a == b {
		t.Fatalf("instanceLockPath collision: %q", a)
	}
	if base := filepath.Base(a); len(base) >= 100 {
		t.Errorf("socket file name %q is too long for a unix socket path", base)
	}
}
