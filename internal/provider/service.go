// Package provider implements the unofficial upstream adapters documented in requirements/03.
// Canonical identities are AniList IDs; provider slugs never become application identities.
package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"myanime/internal/domain"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Error struct {
	Code       string
	Provider   string
	RetryAfter time.Duration
	Cause      error
}

func (e *Error) Error() string     { return e.Provider + ": " + e.Code }
func (e *Error) Unwrap() error     { return e.Cause }
func failure(code, p string) error { return &Error{Code: code, Provider: p} }

type entry struct {
	value any
	err   error
	until time.Time
}
type Service struct {
	client    *http.Client
	mu        sync.Mutex
	cache     map[string]entry
	enabled   map[string]bool
	cooldown  map[string]time.Time
	health    map[string]Health
	sessionMu sync.Mutex
	token     string
	expiry    time.Time
	keys      [3]string
	keysAt    time.Time
	reg       *registry
}

var _ domain.Catalog = (*Service)(nil)

func New() *Service {
	jar, _ := cookiejar.New(nil)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	tr.DialContext = publicDial
	tr.ResponseHeaderTimeout = 8 * time.Second
	tr.MaxConnsPerHost = 8
	c := &http.Client{Transport: tr, Jar: jar, Timeout: 12 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return failure("redirect_limit", "transport")
		}
		return validateURL(r.URL)
	}}
	// The catalogue cache is process-local and starts empty: a restart (or a
	// binary upgrade) never reuses titles fetched by an older taxonomy mapping.
	// That keeps pre-tags entries, which have no genre/tag fields, from
	// lingering in a long-lived cache after the mapping changed.
	s := &Service{client: c, cache: map[string]entry{}, enabled: map[string]bool{}, cooldown: map[string]time.Time{}, health: map[string]Health{}, reg: newRegistry()}
	// Registration is the only wiring an in-process adapter needs.
	for _, adapter := range []AnimeProvider{newHianime(s), newOtakudesu(s), newAnichan(s)} {
		if e := s.RegisterAnime(adapter); e != nil {
			panic("provider: register " + adapter.Identity().ID + ": " + e.Error())
		}
	}
	for _, adapter := range []MangaProvider{newMangadex(s), newMangafire(s), newComix(s)} {
		if e := s.RegisterManga(adapter); e != nil {
			panic("provider: register " + adapter.Identity().ID + ": " + e.Error())
		}
	}
	return s
}
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		return nil, e
	}
	for _, ip := range ips {
		if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
			return nil, failure("forbidden", "transport")
		}
	}
	var last error
	for _, ip := range ips {
		c, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if e == nil {
			return c, nil
		}
		last = e
	}
	return nil, last
}
func validateURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return failure("forbidden", "transport")
	}
	return nil
}
func (s *Service) Refresh() {
	s.mu.Lock()
	s.cache = map[string]entry{}
	s.cooldown = map[string]time.Time{}
	s.mu.Unlock()
}
func (s *Service) SetEnabled(p string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.reg.known(p) {
		return failure("invalid_provider", p)
	}
	s.enabled[p] = on
	s.cache = map[string]entry{}
	return nil
}
func (s *Service) Providers() []domain.ProviderInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.ProviderInfo{}
	for _, p := range s.reg.animeProviders() {
		id := p.Identity().ID
		status := "unverified"
		if !s.enabled[id] {
			status = "disabled"
		}
		out = append(out, domain.ProviderInfo{
			ID:           id,
			Enabled:      s.enabled[id],
			Modes:        p.Modes(),
			Status:       status,
			Capabilities: providerCapabilities(animeCapabilities(p)),
			Health:       providerHealth(s.health[id]),
		})
	}
	// Manga adapters share the same identity space, so they belong in the same
	// list. They carry no audio modes; reader formats come from capabilities.
	for _, p := range s.reg.mangaProviders() {
		id := p.Identity().ID
		status := "unverified"
		if !s.enabled[id] {
			status = "disabled"
		}
		out = append(out, domain.ProviderInfo{
			ID:           id,
			Enabled:      s.enabled[id],
			Modes:        []string{},
			Status:       status,
			Capabilities: providerCapabilities(mangaCapabilities(p)),
			Health:       providerHealth(s.health[id]),
		})
	}
	return out
}

// providerCapabilities projects a typed provider-level capability declaration
// onto the wire shape without implying any title-level availability.
func providerCapabilities(c Capabilities) domain.ProviderCapabilities {
	out := domain.ProviderCapabilities{
		Kinds:         make([]string, 0, len(c.Kinds)),
		AudioModes:    make([]string, 0, len(c.AudioModes)),
		ReaderFormats: make([]string, 0, len(c.ReaderFormats)),
	}
	for _, k := range c.Kinds {
		out.Kinds = append(out.Kinds, string(k))
	}
	for _, m := range c.AudioModes {
		out.AudioModes = append(out.AudioModes, string(m))
	}
	for _, f := range c.ReaderFormats {
		out.ReaderFormats = append(out.ReaderFormats, string(f))
	}
	return out
}

// providerHealth projects the recorded health probe, defaulting to unknown.
func providerHealth(h Health) domain.ProviderHealth {
	if h.State == "" {
		return domain.ProviderHealth{State: string(HealthUnknown)}
	}
	return domain.ProviderHealth{
		State:     string(h.State),
		CheckedAt: h.CheckedAt,
		Reason:    h.Reason,
	}
}

// RegisterAnime adds an in-process anime adapter. Registration is the only
// step needed for search, inventory and playback to reach it.
func (s *Service) RegisterAnime(p AnimeProvider) error {
	if e := s.reg.registerAnime(p); e != nil {
		return e
	}
	s.mu.Lock()
	s.enabled[p.Identity().ID] = true
	s.mu.Unlock()
	return nil
}

// RegisterManga adds an in-process manga adapter and enables it.
func (s *Service) RegisterManga(p MangaProvider) error {
	if e := s.reg.registerManga(p); e != nil {
		return e
	}
	s.mu.Lock()
	s.enabled[p.Identity().ID] = true
	s.mu.Unlock()
	return nil
}

// animeProviders lists registered anime adapters in registration order.
func (s *Service) animeProviders() []AnimeProvider { return s.reg.animeProviders() }

func (s *Service) isEnabled(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled[id]
}
func (s *Service) getCache(k string) (entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.cache[k]
	if ok && time.Now().Before(v.until) {
		return v, true
	}
	delete(s.cache, k)
	return entry{}, false
}
func (s *Service) putCache(k string, v any, e error, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= 512 {
		for key := range s.cache {
			delete(s.cache, key)
			break
		}
	}
	s.cache[k] = entry{v, e, time.Now().Add(ttl)}
}
func (s *Service) request(ctx context.Context, method, raw, ref string, payload any, headers map[string]string) ([]byte, error) {
	return s.requestLimit(ctx, method, raw, ref, payload, headers, 2<<20)
}

// requestLimit is request with an explicit response-size bound. Only large
// single-response metadata (episode titles) needs more than the default.
func (s *Service) requestLimit(ctx context.Context, method, raw, ref string, payload any, headers map[string]string, limit int64) ([]byte, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return nil, e
	}
	if e = validateURL(u); e != nil {
		return nil, e
	}
	s.mu.Lock()
	until := s.cooldown[u.Host]
	s.mu.Unlock()
	if time.Now().Before(until) {
		return nil, &Error{Code: "rate_limited", Provider: u.Host, RetryAfter: time.Until(until)}
	}
	var body []byte
	if payload != nil {
		body, e = json.Marshal(payload)
		if e != nil {
			return nil, e
		}
	}
	for attempt := range 2 {
		r, e := http.NewRequestWithContext(ctx, method, raw, bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		r.Header.Set("User-Agent", "Mozilla/5.0 MyAnime/1.0")
		if ref != "" {
			r.Header.Set("Referer", ref)
			if ru, er := url.Parse(ref); er == nil {
				r.Header.Set("Origin", ru.Scheme+"://"+ru.Host)
			}
		}
		if payload != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		resp, e := s.client.Do(r)
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var ne net.Error
			code := "unavailable"
			if errors.As(e, &ne) && ne.Timeout() {
				code = "timeout"
			}
			if attempt == 0 && method == "GET" {
				continue
			}
			return nil, &Error{Code: code, Provider: u.Host, Cause: e}
		}
		b, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		resp.Body.Close()
		if int64(len(b)) > limit {
			return nil, failure("malformed_source", u.Host)
		}
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return b, nil
		}
		code := "unavailable"
		switch resp.StatusCode {
		case 401:
			code = "auth"
		case 403:
			code = "forbidden"
		case 429:
			code = "rate_limited"
		}
		delay := time.Duration(0)
		if code == "rate_limited" {
			delay = time.Minute
			if sec, er := strconv.Atoi(resp.Header.Get("Retry-After")); er == nil && sec >= 0 {
				delay = time.Duration(sec) * time.Second
			} else if t, er := http.ParseTime(resp.Header.Get("Retry-After")); er == nil {
				delay = time.Until(t)
			}
			s.mu.Lock()
			if len(s.cooldown) >= 128 {
				s.cooldown = map[string]time.Time{}
			}
			s.cooldown[u.Host] = time.Now().Add(delay)
			s.mu.Unlock()
		}
		if resp.StatusCode >= 500 && attempt == 0 && method == "GET" {
			continue
		}
		return nil, &Error{Code: code, Provider: u.Host, RetryAfter: delay}
	}
	return nil, failure("unavailable", u.Host)
}
func (s *Service) get(ctx context.Context, u, ref string) ([]byte, error) {
	return s.request(ctx, "GET", u, ref, nil, nil)
}
func absolute(base, relative string) string {
	if strings.TrimSpace(relative) == "" {
		return ""
	}
	b, e := url.Parse(base)
	if e != nil {
		return ""
	}
	r, e := url.Parse(relative)
	if e != nil {
		return ""
	}
	u := b.ResolveReference(r)
	if validateURL(u) != nil {
		return ""
	}
	return u.String()
}
func origin(raw string) string {
	u, _ := url.Parse(raw)
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/"
}
func animeNumber(id string) (int, error) {
	if !strings.HasPrefix(id, "anilist:") {
		return 0, failure("invalid_mapping", "catalog")
	}
	n, e := strconv.Atoi(strings.TrimPrefix(id, "anilist:"))
	if e != nil || n < 1 {
		return 0, failure("invalid_mapping", "catalog")
	}
	return n, nil
}
func episodeID(a, label string) string {
	return a + ":episode:" + base64.RawURLEncoding.EncodeToString([]byte(label))
}
func episodeLabel(a, id string) (string, error) {
	prefix := a + ":episode:"
	if !strings.HasPrefix(id, prefix) {
		return "", failure("invalid_mapping", "catalog")
	}
	b, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, prefix))
	if e != nil || len(b) == 0 || len(b) > 128 {
		return "", failure("invalid_mapping", "catalog")
	}
	return string(b), nil
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
