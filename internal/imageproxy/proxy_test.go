package imageproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"myanime/internal/domain"
	"myanime/internal/media"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func pngBody() []byte {
	b := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	return append(b, make([]byte, 64)...)
}

func manager(t *testing.T) *Manager {
	t.Helper()
	m := New("", 0)
	t.Cleanup(m.Close)
	return m
}

func grant(t *testing.T, m *Manager, urls ...string) string {
	t.Helper()
	pages := make([]domain.MangaPage, len(urls))
	for i, u := range urls {
		pages[i] = domain.MangaPage{Index: i + 1, URL: u}
	}
	token, e := m.Grant(pages, 0)
	if e != nil {
		t.Fatal(e)
	}
	return token
}

func get(m *Manager, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func errBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out struct {
		Error map[string]any `json:"error"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatalf("non-JSON error body: %q", w.Body.String())
	}
	return out.Error
}

func response(typ string, body []byte) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{typ}}, Body: &nopCloser{bytes: body}}
}

type nopCloser struct {
	bytes []byte
	off   int
}

func (c *nopCloser) Read(p []byte) (int, error) {
	if c.off >= len(c.bytes) {
		return 0, io.EOF
	}
	n := copy(p, c.bytes[c.off:])
	c.off += n
	return n, nil
}
func (c *nopCloser) Close() error { return nil }

func TestServeImageAndCache(t *testing.T) {
	m := manager(t)
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://cdn.example/p1.png" {
			t.Errorf("unexpected upstream %s", r.URL)
		}
		return response("image/png", pngBody()), nil
	})}
	token := grant(t, m, "https://cdn.example/p1.png")
	w := get(m, "/api/v1/manga-pages/"+token+"/1")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || w.Body.Len() != len(pngBody()) {
		t.Fatalf("code=%d type=%s len=%d", w.Code, w.Header().Get("Content-Type"), w.Body.Len())
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}

// Hotlink-protected CDNs (e.g. MangaFire) reject requests without the provider
// origin, while other sources (e.g. Comix) reject requests that carry one. The
// header therefore has to come from the page inventory, never a global guess.
func TestRefererComesFromPageInventory(t *testing.T) {
	m := manager(t)
	seen := map[string]string{}
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		seen[r.URL.String()] = r.Header.Get("Referer")
		return response("image/png", pngBody()), nil
	})}
	pages := []domain.MangaPage{
		{Index: 1, URL: "https://cdn.example/needs.png", Referer: "https://source.example/"},
		{Index: 2, URL: "https://cdn.example/plain.png"},
	}
	token, e := m.Grant(pages, 0)
	if e != nil {
		t.Fatal(e)
	}
	if w := get(m, "/api/v1/manga-pages/"+token+"/1"); w.Code != 200 {
		t.Fatalf("page 1 code %d", w.Code)
	}
	if w := get(m, "/api/v1/manga-pages/"+token+"/2"); w.Code != 200 {
		t.Fatalf("page 2 code %d", w.Code)
	}
	if got := seen["https://cdn.example/needs.png"]; got != "https://source.example/" {
		t.Fatalf("referer = %q, want the provider origin", got)
	}
	if got, ok := seen["https://cdn.example/plain.png"]; !ok || got != "" {
		t.Fatalf("referer leaked to a source that rejects it: %q (seen=%v)", got, ok)
	}

	// A referer that is not a plain HTTPS origin must not be accepted.
	if _, e := m.Grant([]domain.MangaPage{{Index: 1, URL: "https://cdn.example/a.png", Referer: "javascript:alert(1)"}}, 0); !errors.Is(e, ErrInvalid) {
		t.Fatalf("bad referer accepted: %v", e)
	}
}

func TestExpiredCapabilityRejected(t *testing.T) {
	m := manager(t)
	called := false
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		called = true
		return response("image/png", pngBody()), nil
	})}
	pages := []domain.MangaPage{{Index: 1, URL: "https://cdn.example/p1.png"}}
	token, e := m.Grant(pages, -1) // force immediate expiry path
	if e != nil {
		t.Fatal(e)
	}
	// Expired on arrival: any later request must 403.
	time.Sleep(time.Millisecond)
	w := get(m, "/api/v1/manga-pages/"+token+"/1")
	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if e := errBody(t, w); e["code"] != "CAPABILITY_EXPIRED" {
		t.Fatalf("unexpected error body %v", e)
	}
	if called {
		t.Fatal("upstream contacted for expired capability")
	}
	// Unknown token is equivalent.
	w = get(m, "/api/v1/manga-pages/deadbeef/1")
	if w.Code != 403 {
		t.Fatalf("unknown token: expected 403, got %d", w.Code)
	}
}

func TestRevokeCancelsInFlight(t *testing.T) {
	m := manager(t)
	started := make(chan struct{})
	release := make(chan struct{})
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
			return response("image/png", pngBody()), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	token := grant(t, m, "https://cdn.example/slow.png")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- get(m, "/api/v1/manga-pages/"+token+"/1")
	}()
	<-started
	m.Revoke(token)
	w := <-done
	if w.Code != 403 {
		t.Fatalf("revoked capability: expected 403, got %d %s", w.Code, w.Body.String())
	}
}

func TestCancellationAbortsRequest(t *testing.T) {
	m := manager(t)
	started := make(chan struct{})
	release := make(chan struct{})
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
			return response("image/png", pngBody()), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})}
	token := grant(t, m, "https://cdn.example/slow.png")
	req, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
		close(release)
	}()
	w := httptest.NewRecorder()
	rreq := httptest.NewRequest("GET", "/api/v1/manga-pages/"+token+"/1", nil).WithContext(req)
	m.Handler().ServeHTTP(w, rreq)
	if w.Body.Len() != 0 || w.Header().Get("Content-Type") == "image/png" {
		t.Fatalf("canceled request should abort silently, got %d %q", w.Code, w.Body.String())
	}
}

func TestHTMLDisguisedAsImageRejected(t *testing.T) {
	m := manager(t)
	for _, typ := range []string{"image/png", "text/html"} {
		m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			return response(typ, []byte("<html><script>steal()</script></html>")), nil
		})}
		token := grant(t, m, "https://cdn.example/fake.png")
		w := get(m, "/api/v1/manga-pages/"+token+"/1")
		if w.Code != 502 {
			t.Fatalf("content-type %q: expected 502, got %d", typ, w.Code)
		}
		if e := errBody(t, w); e["code"] != "INVALID_IMAGE" {
			t.Fatalf("unexpected error %v", e)
		}
		if w.Header().Get("Content-Type") == "image/png" {
			t.Fatal("HTML body passed as image")
		}
	}
}

func TestMissingPageRecoverable(t *testing.T) {
	m := manager(t)
	token := grant(t, m, "https://cdn.example/p1.png", "https://cdn.example/p2.png")
	w := get(m, "/api/v1/manga-pages/"+token+"/9")
	if w.Code != 404 {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	e := errBody(t, w)
	if e["code"] != "PAGE_NOT_FOUND" || e["retryable"] != true {
		t.Fatalf("expected recoverable PAGE_NOT_FOUND, got %v", e)
	}
}

func TestPrivateNetworkSSRFBlocked(t *testing.T) {
	m := manager(t)
	// Real safe transport: dialing loopback must fail before any
	// connection is established (deterministic, no network).
	for _, target := range []string{
		"https://127.0.0.1/x.png",
		"https://localhost/x.png",
		"https://10.0.0.5/x.png",
		"https://169.254.169.254/latest/meta-data",
	} {
		token := grant(t, m, target)
		w := get(m, "/api/v1/manga-pages/"+token+"/1")
		if w.Code != 502 {
			t.Fatalf("private target %s: expected 502, got %d %s", target, w.Code, w.Body.String())
		}
	}
}

func TestRedirectToPrivateBlocked(t *testing.T) {
	m := manager(t)
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == "https://cdn.example/redirect" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://169.254.169.254/latest"}}, Body: &nopCloser{}}, nil
		}
		t.Fatalf("unexpected request %s", r.URL)
		return nil, nil
	}), CheckRedirect: func(r *http.Request, via []*http.Request) error { return media.ValidateHTTPS(r.URL.String()) }}
	token := grant(t, m, "https://cdn.example/redirect")
	w := get(m, "/api/v1/manga-pages/"+token+"/1")
	if w.Code != 502 {
		t.Fatalf("redirect to private network: expected 502, got %d", w.Code)
	}
}

func TestGrantRejectsInvalidURLs(t *testing.T) {
	m := manager(t)
	for _, pages := range [][]domain.MangaPage{
		{},
		{{Index: 1, URL: "http://cdn.example/p.png"}},
		{{Index: 1, URL: "https://user:pass@cdn.example/p.png"}},
		{{Index: 1, URL: "https://cdn.example:8443/p.png"}},
		{{Index: 5, URL: "https://cdn.example/p.png"}},
	} {
		if _, e := m.Grant(pages, 0); !errors.Is(e, ErrInvalid) {
			t.Fatalf("expected ErrInvalid for %v, got %v", pages, e)
		}
	}
}

func TestSizeBound(t *testing.T) {
	m := manager(t)
	oversized := append(pngBody(), make([]byte, MaxImageBytes)...)
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response("image/png", oversized), nil
	})}
	token := grant(t, m, "https://cdn.example/huge.png")
	w := get(m, "/api/v1/manga-pages/"+token+"/1")
	if w.Code != 502 {
		t.Fatalf("oversized image: expected 502, got %d", w.Code)
	}
	if e := errBody(t, w); e["code"] != "IMAGE_TOO_LARGE" {
		t.Fatalf("expected IMAGE_TOO_LARGE, got %v", e)
	}
}

func TestDiskCacheServesWithoutUpstream(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, 0)
	t.Cleanup(m.Close)
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response("image/png", pngBody()), nil
	})}
	token := grant(t, m, "https://cdn.example/p1.png")
	if w := get(m, "/api/v1/manga-pages/"+token+"/1"); w.Code != 200 {
		t.Fatalf("first fetch failed: %d", w.Code)
	}
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		t.Fatal("cache miss should not hit upstream")
		return nil, nil
	})}
	if w := get(m, "/api/v1/manga-pages/"+token+"/1"); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("cache hit failed: %d", w.Code)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected one cache entry, got %d", len(entries))
	}
}

func TestDiskCacheBounded(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, int64(len(pngBody())*2+1))
	t.Cleanup(m.Close)
	var mu sync.Mutex
	served := 0
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		served++
		mu.Unlock()
		return response("image/png", pngBody()), nil
	})}
	for i := range 6 {
		token := grant(t, m, "https://cdn.example/p"+string(rune('a'+i))+".png")
		if w := get(m, "/api/v1/manga-pages/"+token+"/1"); w.Code != 200 {
			t.Fatalf("fetch %d failed: %d", i, w.Code)
		}
	}
	var total int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if total > m.budget {
		t.Fatalf("cache %d exceeds budget %d", total, m.budget)
	}
}

func TestHEADAndMethodPolicy(t *testing.T) {
	m := manager(t)
	m.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return response("image/png", pngBody()), nil
	})}
	token := grant(t, m, "https://cdn.example/p1.png")
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("HEAD", "/api/v1/manga-pages/"+token+"/1", nil))
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD: code=%d body=%d", w.Code, w.Body.Len())
	}
	w = httptest.NewRecorder()
	m.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/manga-pages/"+token+"/1", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: code=%d", w.Code)
	}
	if w := get(m, "/api/v1/manga-pages/"+token+"/1?q=1"); w.Code != 404 {
		t.Fatalf("query must be rejected: %d", w.Code)
	}
	if w := get(m, "/api/v1/manga-pages/"+token+"/x"); w.Code != 404 {
		t.Fatalf("bad index: %d", w.Code)
	}
}

func TestCloseCancelsCapabilities(t *testing.T) {
	m := New("", 0)
	token := grant(t, m, "https://cdn.example/p1.png")
	m.Close()
	w := get(m, "/api/v1/manga-pages/"+token+"/1")
	if w.Code != 403 {
		t.Fatalf("after Close: expected 403, got %d", w.Code)
	}
}
