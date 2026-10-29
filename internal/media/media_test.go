package media

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"io"
	"myanime/internal/domain"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T) (*Manager, Session) {
	t.Helper()
	m := New()
	t.Cleanup(m.Close)
	s, e := m.Create(context.Background(), domain.Source{MasterURL: "https://cdn.example/master.m3u8", Provider: "hianime", Mode: "SUB"}, "alice", "")
	if e != nil {
		t.Fatal(e)
	}
	return m, s
}
func TestPublicIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "::1", "fc00::1", "fe80::1", "::ffff:8.8.8.8", "2001:db8::1", "224.0.0.1"} {
		if publicIP(netip.MustParseAddr(s)) {
			t.Errorf("allowed %s", s)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public rejected")
	}
}
func TestRewrite(t *testing.T) {
	m, s := fixture(t)
	state := m.sessions[m.ids[s.ID]]
	base, _ := url.Parse("https://cdn.example/a/master.m3u8?auth=secret")
	out, e := rewrite(state, base, []byte("#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,URI=\"audio.m3u8?q=1\"\n#EXT-X-KEY:METHOD=AES-128,URI=\"/key?k=2\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-BYTERANGE:20@5\nsegment.ts?q=3\n#EXT-X-DISCONTINUITY\n"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Count(string(out), "/media/") != 4 || !strings.Contains(string(out), "#EXT-X-BYTERANGE:20@5") {
		t.Fatal(string(out))
	}
	if _, ok := state.urls["https://cdn.example/a/segment.ts?q=3"]; !ok {
		t.Fatal("query lost")
	}
}
func TestRangeHeadAndRevoke(t *testing.T) {
	m, s := fixture(t)
	st := m.sessions[m.ids[s.ID]]
	local, e := st.register("https://cdn.example/file.mp4", false)
	if e != nil {
		t.Fatal(e)
	}
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Range") != "bytes=2-4" || r.Header.Get("If-Range") != "etag" {
			t.Error("range headers lost")
		}
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Type": []string{"video/mp4"}, "Content-Range": []string{"bytes 2-4/8"}}, Body: io.NopCloser(strings.NewReader("abc")), Request: r}, nil
	})}
	for _, method := range []string{"GET", "HEAD"} {
		r := httptest.NewRequest(method, local, nil)
		r.Header.Set("Range", "bytes=2-4")
		r.Header.Set("If-Range", "etag")
		w := httptest.NewRecorder()
		m.Handler().ServeHTTP(w, r)
		if w.Code != 206 || w.Header().Get("Content-Range") != "bytes 2-4/8" {
			t.Fatal(w)
		}
		if method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
	}
	if m.Revoke(s.ID, "bob") {
		t.Fatal("wrong owner revoked")
	}
	m.RevokeOwner("alice")
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", local, nil))
	if w.Code != 410 {
		t.Fatal(w.Code)
	}
}
func TestExpirationAndSubtitle(t *testing.T) {
	m, s := fixture(t)
	for _, v := range []string{"WEBVTT\n\n00:00.000 --> 00:01.000\n<script>alert(1)</script>\n", "WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nHello\n"} {
		if _, e := m.AddSubtitle(s.ID, "alice", "Imported", []byte(v)); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []string{"WEBVTT\n\n00:99.000 --> 00:01.000\nBad", "WEBVTT\n\nSTYLE\n::cue { color: red; }", "not vtt"} {
		if validateVTT([]byte(v)) == nil {
			t.Fatal("accepted invalid subtitle")
		}
	}
	m.sessions[m.ids[s.ID]].idle = time.Now().Add(-time.Second)
	if _, e := m.AddSubtitle(s.ID, "alice", "x", []byte("WEBVTT\n")); e != ErrExpired {
		t.Fatal(e)
	}
}
func TestEncryptedSegment(t *testing.T) {
	plain := []byte("https://cdn.example/segment.ts?x=1")
	pad := 16 - len(plain)%16
	for range pad {
		plain = append(plain, byte(pad))
	}
	block, _ := aes.NewCipher(append([]byte("i?LMTAx0Q6,:}50U"), make([]byte, 16)...))
	cipher.NewCBCEncrypter(block, []byte("W0;27ToaUpl_P%'c")).CryptBlocks(plain, plain)
	u, _ := url.Parse("https://megaplay.example/segment/" + base64.RawURLEncoding.EncodeToString(plain))
	got, strip, e := segmentURL("hianime", u)
	if e != nil || strip || got != "https://cdn.example/segment.ts?x=1" {
		t.Fatal(got, e)
	}
	other, _, _ := segmentURL("otakudesu", u)
	if other != u.String() {
		t.Fatal("codec crossed provider boundary")
	}
}
func TestRedirectPolicy(t *testing.T) {
	c := secureClient()
	r := httptest.NewRequest("GET", "http://169.254.169.254/latest/meta-data", nil)
	if c.CheckRedirect(r, nil) == nil {
		t.Fatal("insecure redirect allowed")
	}
	c.CloseIdleConnections()
}
func TestSegmentTransformProvenance(t *testing.T) {
	m, s := fixture(t)
	st := m.sessions[m.ids[s.ID]]
	raw := "https://megaplay.buzz/segment/invalid"
	if _, e := st.register(raw, false); e != nil {
		t.Fatal("generic source incorrectly decoded", e)
	}
	st.referer = "https://megaplay.buzz/stream/1"
	if _, e := st.register(raw+"2", false); e == nil {
		t.Fatal("Megaplay malformed token accepted")
	}
	st.ActualProvider = "anichan"
	if _, e := st.register(raw+"3", false); e == nil {
		t.Fatal("AniChan Megaplay malformed token accepted")
	}
}
func TestExtensionlessManifestAndOrigin(t *testing.T) {
	m, s := fixture(t)
	st := m.sessions[m.ids[s.ID]]
	st.referer = "https://megaplay.buzz/watch/1"
	local, e := st.register("https://cdn.example/api/hls?id=1", false)
	if e != nil {
		t.Fatal(e)
	}
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Origin") != "https://megaplay.buzz" || r.Header.Get("Referer") != st.referer {
			t.Error("adapter headers missing")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/octet-stream"}}, Body: io.NopCloser(strings.NewReader("#EXTM3U\n#EXTINF:5,\nfile.ts?token=private\n")), Request: r}, nil
	})}
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", local, nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/media/") || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
	if cap(m.slots) != 8 {
		t.Fatal("concurrency limit")
	}
}

func TestVerifiedTransportStreamMIME(t *testing.T) {
	for _, valid := range []bool{true, false} {
		m, s := fixture(t)
		st := m.sessions[m.ids[s.ID]]
		local, err := st.register("https://cdn.example/wrapped.jpg", false)
		if err != nil {
			t.Fatal(err)
		}
		data := make([]byte, 564)
		data[0], data[188] = 0x47, 0x47
		if valid {
			data[376] = 0x47
		}
		m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader(string(data))), Request: r}, nil
		})}
		w := httptest.NewRecorder()
		m.Handler().ServeHTTP(w, httptest.NewRequest("GET", local, nil))
		want := "image/jpeg"
		if valid {
			want = "video/mp2t"
		}
		if w.Code != 200 || w.Header().Get("Content-Type") != want || w.Body.String() != string(data) {
			t.Fatalf("valid=%v status=%d content-type=%s bytes=%d", valid, w.Code, w.Header().Get("Content-Type"), w.Body.Len())
		}
	}
}

func TestSessionVariantCapability(t *testing.T) {
	m := New()
	defer m.Close()
	s, err := m.Create(context.Background(), domain.Source{MasterURL: "https://cdn.example/master.m3u8", Provider: "otakudesu", Mode: "SUB", Hardsub: true, HardsubLanguage: "id", Variants: []domain.Variant{{ID: "720", Label: "720p", Height: 720, URL: "https://cdn.example/720.m3u8?secret=hidden"}}}, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Variants) != 1 || !strings.HasPrefix(s.Variants[0].URL, "/media/") || strings.Contains(s.Variants[0].URL, "hidden") || s.Variants[0].Height != 720 || !s.Hardsub || s.HardsubLanguage != "id" {
		t.Fatal("variant capability or source metadata missing")
	}
}

// Media-proxy failures carry the stable JSON shape with the failing stage and
// a request ID, so the player can retry only the failed step.
func TestStagedMediaFailures(t *testing.T) {
	m, s := fixture(t)
	st := m.sessions[m.ids[s.ID]]
	if _, e := st.register("https://cdn.example/media.m3u8", false); e != nil {
		t.Fatal(e)
	}
	if _, e := st.register("https://cdn.example/seg0.ts", false); e != nil {
		t.Fatal(e)
	}
	token := m.ids[s.ID]
	h := m.Handler()
	// Unknown session: session stage.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/media/unknown-token/1", nil))
	if w.Code != 410 || !strings.Contains(w.Body.String(), `"stage":"session"`) || !strings.Contains(w.Body.String(), `"requestId"`) {
		t.Fatalf("unknown session: %d %s", w.Code, w.Body.String())
	}
	// Unknown resource inside a live session: segment-stage media miss.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/media/"+token+"/99", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"stage":"segment"`) {
		t.Fatalf("unknown resource: %d %s", w.Code, w.Body.String())
	}
	// Upstream failure: the stage follows the resource kind, manifest or
	// segment, and the failure is retryable.
	m.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 502, Status: "502 Bad Gateway", Header: http.Header{}, Body: io.NopCloser(strings.NewReader("nope"))}, nil
	})
	for _, tc := range []struct{ path, stage string }{{"/media/" + token + "/1", "manifest"}, {"/media/" + token + "/3", "segment"}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		body := w.Body.String()
		if w.Code != 502 || !strings.Contains(body, `"stage":"`+tc.stage+`"`) || !strings.Contains(body, `"retryable":true`) || !strings.Contains(body, "UPSTREAM_UNAVAILABLE") {
			t.Fatalf("%s: %d %s", tc.path, w.Code, body)
		}
	}
}
