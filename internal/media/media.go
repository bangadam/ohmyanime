package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"myanime/internal/domain"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrExpired = errors.New("playback session expired")
var ErrInvalid = errors.New("invalid media resource")
var ErrLimit = errors.New("media capacity exceeded")

const maxBody = 2 << 20
const maxResources = 20000

type Variant struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Height int    `json:"height,omitempty"`
	URL    string `json:"url"`
}

type Session struct {
	ID              string            `json:"id"`
	MasterURL       string            `json:"masterUrl"`
	ActualProvider  string            `json:"actualProvider"`
	ActualMode      string            `json:"actualMode"`
	Hardsub         bool              `json:"hardsub"`
	HardsubLanguage string            `json:"hardsubLanguage"`
	Variants        []Variant         `json:"variants"`
	Subtitles       []domain.Subtitle `json:"subtitles"`
	ExpiresAt       time.Time         `json:"expiresAt"`
	Intro           *domain.Interval  `json:"intro"`
	Outro           *domain.Interval  `json:"outro"`
}
type resource struct {
	url      string
	data     []byte
	subtitle bool
	strip    bool
}
type state struct {
	Session
	token, owner, base, referer string
	idle                        time.Time
	resources                   map[string]resource
	urls                        map[string]string
	ctx                         context.Context
	cancel                      context.CancelFunc
	uploads                     int
}
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*state
	ids      map[string]string
	client   *http.Client
	stop     chan struct{}
	closed   bool
	slots    chan struct{}
}

func New() *Manager {
	m := &Manager{sessions: map[string]*state{}, ids: map[string]string{}, client: secureClient(), stop: make(chan struct{}), slots: make(chan struct{}, 8)}
	go m.reap()
	return m
}
func randomID() (string, error) {
	b := make([]byte, 24)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func (m *Manager) reap() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case now := <-t.C:
			m.mu.Lock()
			m.expire(now)
			m.mu.Unlock()
		case <-m.stop:
			return
		}
	}
}
func (m *Manager) expire(now time.Time) {
	for token, s := range m.sessions {
		if !now.Before(s.idle) || !now.Before(s.ExpiresAt) {
			s.cancel()
			delete(m.sessions, token)
			delete(m.ids, s.ID)
		}
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.closed = true
	close(m.stop)
	for _, s := range m.sessions {
		s.cancel()
	}
	clear(m.sessions)
	clear(m.ids)
	m.client.CloseIdleConnections()
}
func (m *Manager) Revoke(id, owner string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	token := m.ids[id]
	s := m.sessions[token]
	if s == nil || s.owner != owner {
		return false
	}
	s.cancel()
	delete(m.sessions, token)
	delete(m.ids, id)
	return true
}
func (m *Manager) RevokeOwner(owner string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for token, s := range m.sessions {
		if s.owner == owner {
			s.cancel()
			delete(m.ids, s.ID)
			delete(m.sessions, token)
		}
	}
}
func (s *state) register(raw string, subtitle bool) (string, error) {
	u, e := validURL(raw)
	if e != nil {
		return "", e
	}
	raw = u.String()
	if id, ok := s.urls[raw]; ok {
		return s.base + "/media/" + s.token + "/" + id, nil
	}
	if len(s.resources) >= maxResources {
		return "", ErrLimit
	}
	id := strconv.Itoa(len(s.resources) + 1)
	codec := ""
	if ref, err := validURL(s.referer); err == nil && !subtitle && (s.ActualProvider == "hianime" || s.ActualProvider == "anichan") {
		host := strings.ToLower(ref.Hostname())
		if host == "megaplay.buzz" || strings.HasSuffix(host, ".megaplay.buzz") {
			codec = "hianime"
		}
	}
	decoded, strip, e := segmentURL(codec, u)
	if e != nil {
		return "", e
	}
	s.resources[id] = resource{url: decoded, subtitle: subtitle, strip: strip}
	s.urls[raw] = id
	return s.base + "/media/" + s.token + "/" + id, nil
}
func (m *Manager) Create(ctx context.Context, source domain.Source, owner, baseURL string) (Session, error) {
	if e := ctx.Err(); e != nil {
		return Session{}, e
	}
	if owner == "" {
		return Session{}, ErrInvalid
	}
	if baseURL != "" {
		u, e := url.Parse(baseURL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
			return Session{}, ErrInvalid
		}
	}
	id, e := randomID()
	if e != nil {
		return Session{}, e
	}
	token, e := randomID()
	if e != nil {
		return Session{}, e
	}
	now := time.Now()
	life, cancel := context.WithCancel(context.Background())
	s := &state{Session: Session{ID: id, ActualProvider: source.Provider, ActualMode: source.Mode, Hardsub: source.Hardsub, HardsubLanguage: source.HardsubLanguage, Variants: make([]Variant, 0, len(source.Variants)), Subtitles: append([]domain.Subtitle{}, source.Subtitles...), ExpiresAt: now.Add(6 * time.Hour), Intro: source.Intro, Outro: source.Outro}, token: token, owner: owner, base: strings.TrimRight(baseURL, "/"), referer: source.Referer, idle: now.Add(30 * time.Minute), resources: map[string]resource{}, urls: map[string]string{}, ctx: life, cancel: cancel}
	success := false
	defer func() {
		if !success {
			cancel()
		}
	}()
	s.MasterURL, e = s.register(source.MasterURL, false)
	if e != nil {
		return Session{}, e
	}
	for _, v := range source.Variants {
		if v.URL == "" {
			continue
		}
		local, err := s.register(v.URL, false)
		if err != nil {
			return Session{}, err
		}
		s.Variants = append(s.Variants, Variant{ID: v.ID, Label: v.Label, Height: v.Height, URL: local})
	}
	for i, t := range s.Subtitles {
		u, e := s.register(t.URL, true)
		if e != nil {
			return Session{}, e
		}
		s.Subtitles[i].URL = u
		s.Subtitles[i].ID = strconv.Itoa(i + 1)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expire(now)
	if m.closed {
		return Session{}, ErrExpired
	}
	if len(m.sessions) >= 256 {
		return Session{}, ErrLimit
	}
	if e := ctx.Err(); e != nil {
		return Session{}, e
	}
	m.sessions[token] = s
	m.ids[id] = token
	success = true
	return s.Session, nil
}
func (m *Manager) Handler() http.Handler { return http.HandlerFunc(m.serve) }
func (m *Manager) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "HEAD" {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", 405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/media/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/media/") || len(parts) != 2 || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	m.mu.Lock()
	m.expire(time.Now())
	s := m.sessions[parts[0]]
	if s == nil {
		m.mu.Unlock()
		http.Error(w, "session expired", 410)
		return
	}
	res, ok := s.resources[parts[1]]
	if !ok {
		m.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	s.idle = time.Now().Add(30 * time.Minute)
	m.mu.Unlock()
	if res.data != nil {
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		http.ServeContent(w, r, "subtitle.vtt", time.Time{}, bytes.NewReader(res.data))
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	case <-ctx.Done():
		return
	}
	method := r.Method
	if res.strip {
		method = "GET"
	}
	req, e := http.NewRequestWithContext(ctx, method, res.url, nil)
	if e != nil {
		http.Error(w, "invalid media", 502)
		return
	}
	if ref, e := validURL(s.referer); e == nil {
		req.Header.Set("Referer", s.referer)
		req.Header.Set("Origin", ref.Scheme+"://"+ref.Host)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	if !res.strip {
		for _, h := range []string{"Range", "If-Range"} {
			if v := r.Header.Get(h); v != "" {
				req.Header.Set(h, v)
			}
		}
	}
	resp, e := m.client.Do(req)
	if e != nil {
		http.Error(w, "upstream unavailable", 502)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		if resp.StatusCode == 416 {
			w.Header().Set("Content-Range", resp.Header.Get("Content-Range"))
			w.WriteHeader(416)
		} else {
			http.Error(w, "upstream unavailable", 502)
		}
		return
	}
	idleTimer := time.AfterFunc(30*time.Second, cancel)
	idleTimer.Stop()
	defer idleTimer.Stop()
	resp.Body = &idleBody{ReadCloser: resp.Body, timer: idleTimer}
	typ := strings.ToLower(resp.Header.Get("Content-Type"))
	manifest := strings.Contains(typ, "mpegurl") || strings.HasSuffix(strings.ToLower(resp.Request.URL.Path), ".m3u8")
	if r.Method != "HEAD" && !res.strip && !res.subtitle {
		reader := bufio.NewReaderSize(resp.Body, 4096)
		signature, _ := reader.Peek(7)
		manifest = manifest || bytes.Equal(signature, []byte("#EXTM3U"))
		if !manifest {
			packets, _ := reader.Peek(377)
			if len(packets) >= 377 && packets[0] == 0x47 && packets[188] == 0x47 && packets[376] == 0x47 {
				resp.Header.Set("Content-Type", "video/mp2t")
			}
		}
		resp.Body = &bufferedBody{Reader: reader, Closer: resp.Body}
	}
	if manifest && r.Method != "HEAD" {
		data, e := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		if e != nil || len(data) > maxBody {
			http.Error(w, "invalid manifest", 502)
			return
		}
		m.mu.Lock()
		if s.ctx.Err() != nil {
			m.mu.Unlock()
			http.Error(w, "session expired", 410)
			return
		}
		data, e = rewrite(s, resp.Request.URL, data)
		m.mu.Unlock()
		if e != nil {
			http.Error(w, "invalid manifest", 502)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(200)
		_, _ = w.Write(data)
		return
	}
	if res.subtitle && r.Method != "HEAD" {
		data, e := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
		if e != nil || validateVTT(data) != nil {
			http.Error(w, "invalid subtitle", 502)
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write(data)
		return
	}
	if res.strip {
		serveStripped(w, r, resp)
		return
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	if manifest {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Del("Content-Length")
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method != "HEAD" {
		_, _ = io.Copy(w, resp.Body)
	}
}

type idleBody struct {
	io.ReadCloser
	timer *time.Timer
}

func (b *idleBody) Read(p []byte) (int, error) {
	b.timer.Reset(30 * time.Second)
	defer b.timer.Stop()
	return b.ReadCloser.Read(p)
}

type bufferedBody struct {
	io.Reader
	io.Closer
}
