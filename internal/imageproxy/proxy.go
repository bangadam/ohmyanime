// Package imageproxy serves manga page images through a bounded,
// capability-scoped local proxy. Upstream URLs are never exposed to the
// client; each resolved chapter page inventory is minted as a short-lived
// capability token whose pages can then be fetched as
// /api/v1/manga-pages/<token>/<index>.
package imageproxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"myanime/internal/domain"
	"myanime/internal/media"
)

var (
	// ErrExpired reports an unknown, revoked, or expired capability token.
	ErrExpired = errors.New("manga page capability expired")
	// ErrNotFound reports a page index outside the granted inventory.
	ErrNotFound = errors.New("manga page not found")
	// ErrInvalid reports malformed upstream data (bad URL, non-image
	// content, or an image exceeding the size bound).
	ErrInvalid = errors.New("invalid manga page image")
	// ErrUnavailable reports upstream failures.
	ErrUnavailable = errors.New("manga page upstream unavailable")
)

const (
	// MaxImageBytes bounds a single proxied image.
	MaxImageBytes = 16 << 20
	// maxCapabilities bounds concurrently live capability tokens.
	maxCapabilities = 128
	// defaultTTL is the capability lifetime when Grant is given none.
	defaultTTL = 2 * time.Hour
	// maxConcurrency bounds simultaneous upstream fetches.
	maxConcurrency = 4
)

type capabilityPage struct {
	url     string
	referer string
}

type capability struct {
	pages   []capabilityPage
	expires time.Time
	ctx     context.Context
	cancel  context.CancelFunc
}

// Manager owns capability tokens, the bounded upstream client, and a
// bounded on-disk image cache.
type Manager struct {
	mu      sync.Mutex
	caps    map[string]*capability
	client  *http.Client
	slots   chan struct{}
	dir     string
	budget  int64
	used    int64
	sizes   map[string]int64
	stop    chan struct{}
	stopped sync.Once
}

// New creates a Manager. dir is the on-disk cache directory (created on
// demand); an empty dir disables disk caching. budget bounds the total
// bytes held in dir; <=0 uses 256 MiB.
func New(dir string, budget int64) *Manager {
	if budget <= 0 {
		budget = 256 << 20
	}
	m := &Manager{
		caps:   map[string]*capability{},
		client: media.SafeClient(),
		slots:  make(chan struct{}, maxConcurrency),
		dir:    dir,
		budget: budget,
		sizes:  map[string]int64{},
		stop:   make(chan struct{}),
	}
	go m.reap()
	return m
}

// Close cancels every live capability and stops background work.
func (m *Manager) Close() {
	m.stopped.Do(func() { close(m.stop) })
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.caps {
		c.cancel()
	}
	clear(m.caps)
	m.client.CloseIdleConnections()
}

// Revoke cancels one capability token immediately; in-flight fetches for
// it abort.
func (m *Manager) Revoke(token string) {
	m.mu.Lock()
	c := m.caps[token]
	delete(m.caps, token)
	m.mu.Unlock()
	if c != nil {
		c.cancel()
	}
}

// Grant mints a capability token covering the given page inventory and
// returns it. ttl <= 0 uses defaultTTL. Every upstream URL must satisfy the
// shared HTTPS transport policy.
func (m *Manager) Grant(pages []domain.MangaPage, ttl time.Duration) (string, error) {
	if len(pages) == 0 || len(pages) > 1000 {
		return "", ErrInvalid
	}
	if ttl == 0 {
		ttl = defaultTTL
	}
	pagesOut := make([]capabilityPage, len(pages))
	for _, p := range pages {
		if p.Index < 1 || p.Index > len(pages) {
			return "", ErrInvalid
		}
		if media.ValidateHTTPS(p.URL) != nil {
			return "", ErrInvalid
		}
		// A referer is a header, not a fetch target, but it still has to be a
		// plain HTTPS origin so a malformed inventory cannot inject junk.
		if p.Referer != "" && media.ValidateHTTPS(p.Referer) != nil {
			return "", ErrInvalid
		}
		pagesOut[p.Index-1] = capabilityPage{url: p.URL, referer: p.Referer}
	}
	buf := make([]byte, 24)
	if _, e := rand.Read(buf); e != nil {
		return "", e
	}
	token := hex.EncodeToString(buf)
	ctx, cancel := context.WithCancel(context.Background())
	c := &capability{pages: pagesOut, expires: time.Now().Add(ttl), ctx: ctx, cancel: cancel}
	m.mu.Lock()
	m.expireLocked(time.Now())
	for len(m.caps) >= maxCapabilities {
		var oldest string
		var oldestAt time.Time
		for t, cc := range m.caps {
			if oldest == "" || cc.expires.Before(oldestAt) {
				oldest, oldestAt = t, cc.expires
			}
		}
		if cc := m.caps[oldest]; cc != nil {
			cc.cancel()
		}
		delete(m.caps, oldest)
	}
	m.caps[token] = c
	m.mu.Unlock()
	return token, nil
}

func (m *Manager) expireLocked(now time.Time) {
	for t, c := range m.caps {
		if !now.Before(c.expires) {
			c.cancel()
			delete(m.caps, t)
		}
	}
}

// reap cancels expired capabilities even when nobody is requesting them,
// so abandoned preloads do not linger.
func (m *Manager) reap() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-t.C:
			m.mu.Lock()
			m.expireLocked(now)
			m.mu.Unlock()
		}
	}
}

// Handler serves GET/HEAD /api/v1/manga-pages/{token}/{index}.
func (m *Manager) Handler() http.Handler { return http.HandlerFunc(m.serve) }

func (m *Manager) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		fail(w, 405, "METHOD_NOT_ALLOWED", "Method not allowed", false)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/manga-pages/")
	if rest == r.URL.Path || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	index, e := strconv.Atoi(parts[1])
	if e != nil {
		http.NotFound(w, r)
		return
	}
	m.mu.Lock()
	m.expireLocked(time.Now())
	c := m.caps[parts[0]]
	if c == nil {
		m.mu.Unlock()
		fail(w, 403, "CAPABILITY_EXPIRED", "Manga page capability expired or revoked", false)
		return
	}
	if index < 1 || index > len(c.pages) {
		m.mu.Unlock()
		// Missing page: recoverable — the client can re-resolve the
		// chapter inventory and retry.
		fail(w, 404, "PAGE_NOT_FOUND", "Page not in chapter inventory", true)
		return
	}
	page := c.pages[index-1]
	target := page.url
	expires := c.expires
	m.mu.Unlock()

	if data, typ, ok := m.cacheGet(target); ok {
		serveImage(w, r, data, typ, expires)
		return
	}

	// Bind the request to both the client connection and the capability
	// lifetime: revocation or expiry aborts in-flight fetches.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()

	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	case <-ctx.Done():
		return
	}

	req, e := http.NewRequestWithContext(ctx, r.Method, target, nil)
	if e != nil {
		fail(w, 502, "INVALID_IMAGE", "Invalid upstream image URL", false)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	// Some image CDNs reject hotlinked requests without the provider origin.
	// Providers that need nothing leave this empty; a wrong value can break
	// otherwise-readable sources, so it is never guessed globally.
	if page.referer != "" {
		req.Header.Set("Referer", page.referer)
	}
	resp, e := m.client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			if c.ctx.Err() != nil && r.Context().Err() == nil {
				fail(w, 403, "CAPABILITY_EXPIRED", "Manga page capability expired or revoked", false)
			}
			// The client went away: abort silently.
			return
		}
		fail(w, 502, "UPSTREAM_UNAVAILABLE", "Image source unavailable", true)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, 502, "UPSTREAM_UNAVAILABLE", "Image source unavailable", true)
		return
	}
	if typ := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])); !strings.HasPrefix(typ, "image/") {
		fail(w, 502, "INVALID_IMAGE", "Upstream did not return an image", false)
		return
	}
	if resp.ContentLength > MaxImageBytes {
		fail(w, 502, "IMAGE_TOO_LARGE", "Image exceeds size limit", false)
		return
	}
	body := io.LimitReader(resp.Body, MaxImageBytes+1)
	data, e := io.ReadAll(body)
	if e != nil {
		if ctx.Err() != nil {
			if c.ctx.Err() != nil && r.Context().Err() == nil {
				fail(w, 403, "CAPABILITY_EXPIRED", "Manga page capability expired or revoked", false)
			}
			return
		}
		fail(w, 502, "UPSTREAM_UNAVAILABLE", "Image source unavailable", true)
		return
	}
	if len(data) > MaxImageBytes {
		fail(w, 502, "IMAGE_TOO_LARGE", "Image exceeds size limit", false)
		return
	}
	sniffed, ok := sniff(data)
	if !ok {
		// HTML or anything else disguised as an image is rejected.
		fail(w, 502, "INVALID_IMAGE", "Upstream did not return an image", false)
		return
	}
	m.cachePut(target, data)
	serveImage(w, r, data, sniffed, expires)
}

func serveImage(w http.ResponseWriter, r *http.Request, data []byte, typ string, expires time.Time) {
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Capability-Expires", expires.UTC().Format(http.TimeFormat))
	if r.Method == http.MethodHead {
		w.WriteHeader(200)
		return
	}
	_, _ = w.Write(data)
}

var (
	jpegSig = []byte{0xFF, 0xD8, 0xFF}
	pngSig  = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
)

// sniff verifies magic bytes and returns the concrete image media type.
func sniff(data []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(data, jpegSig):
		return "image/jpeg", true
	case bytes.HasPrefix(data, pngSig):
		return "image/png", true
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif", true
	case len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp", true
	}
	return "", false
}

func (m *Manager) cacheName(target string) string {
	sum := sha256.Sum256([]byte(target))
	return hex.EncodeToString(sum[:])
}

func (m *Manager) cacheGet(target string) ([]byte, string, bool) {
	if m.dir == "" {
		return nil, "", false
	}
	path := filepath.Join(m.dir, m.cacheName(target))
	data, e := os.ReadFile(path)
	if e != nil {
		return nil, "", false
	}
	typ, ok := sniff(data)
	if !ok {
		_ = os.Remove(path)
		return nil, "", false
	}
	_ = os.Chtimes(path, time.Now(), time.Now())
	return data, typ, true
}

func (m *Manager) cachePut(target string, data []byte) {
	if m.dir == "" || int64(len(data)) > m.budget {
		return
	}
	name := m.cacheName(target)
	path := filepath.Join(m.dir, name)
	if e := os.MkdirAll(m.dir, 0o700); e != nil {
		return
	}
	tmp := path + ".tmp"
	if e := os.WriteFile(tmp, data, 0o600); e != nil {
		return
	}
	if e := os.Rename(tmp, path); e != nil {
		_ = os.Remove(tmp)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sizes[name]; !exists {
		m.sizes[name] = int64(len(data))
		m.used += int64(len(data))
	}
	m.evictLocked()
}

// evictLocked drops least-recently-used cached files until the cache fits
// its budget. Callers hold m.mu.
func (m *Manager) evictLocked() {
	for m.used > m.budget {
		var oldest string
		var oldestAt time.Time
		for name := range m.sizes {
			info, e := os.Stat(filepath.Join(m.dir, name))
			if e != nil {
				continue
			}
			if oldest == "" || info.ModTime().Before(oldestAt) {
				oldest, oldestAt = name, info.ModTime()
			}
		}
		if oldest == "" {
			return
		}
		_ = os.Remove(filepath.Join(m.dir, oldest))
		m.used -= m.sizes[oldest]
		delete(m.sizes, oldest)
	}
}

func fail(w http.ResponseWriter, status int, code, msg string, retryable bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": code, "message": msg, "retryable": retryable},
	})
}
