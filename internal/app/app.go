package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"myanime/internal/domain"
	"myanime/internal/media"
	"myanime/internal/provider"
	"myanime/internal/store"
)

// GoogleConfig configures the only accepted sign-in method. Web and desktop use
// separate OAuth clients because Google only permits loopback redirects for
// installed-app clients.
type GoogleConfig struct {
	ClientID, ClientSecret               string
	DesktopClientID, DesktopClientSecret string
	AllowedEmails, AllowedDomains        []string
}

type Config struct {
	DataDir, FrontendDir, Mode string
	PublicOrigin, DevOrigin    string
	Development                bool
	Google                     GoogleConfig
	TrustedProxyCIDRs          []string
	FrontendFS                 fs.FS
}

type loginSession struct {
	csrf          string
	email         string
	created, last time.Time
}
type App struct {
	cfg          Config
	db           *store.Store
	catalog      *provider.Service
	media        *media.Manager
	token        string
	mu           sync.Mutex
	sessions     map[string]loginSession
	flows        map[string]*oauthFlow
	mux          *http.ServeMux
	trusted      []*net.IPNet
	resolveSlots chan struct{}
	exchange     func(context.Context, string, string, string, string, string) (string, error)
	verify       func(context.Context, string, string) (googleIdentity, error)
}
type principal struct{ owner, csrf, email string }

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func New(cfg Config) (*App, error) {
	if cfg.Mode == "" {
		cfg.Mode = "web"
	}
	if cfg.Mode != "web" && cfg.Mode != "desktop" {
		return nil, errors.New("invalid mode")
	}
	if cfg.PublicOrigin != "" {
		u, e := url.Parse(cfg.PublicOrigin)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("PublicOrigin must be an HTTPS origin")
		}
	}
	var proxies []*net.IPNet
	for _, cidr := range cfg.TrustedProxyCIDRs {
		_, network, e := net.ParseCIDR(cidr)
		if e != nil {
			return nil, e
		}
		proxies = append(proxies, network)
	}
	if cfg.DataDir == "" {
		p, e := os.UserConfigDir()
		if e != nil {
			return nil, e
		}
		cfg.DataDir = filepath.Join(p, "myanime")
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, err
	}
	db, err := store.Open(filepath.Join(cfg.DataDir, "myanime.db"))
	if err != nil {
		return nil, err
	}
	a := &App{
		cfg: cfg, db: db, catalog: provider.New(), media: media.New(), token: randomToken(),
		sessions: make(map[string]loginSession), flows: make(map[string]*oauthFlow), mux: http.NewServeMux(),
		exchange: exchangeCode, verify: verifyGoogleIDToken,
	}
	a.trusted = proxies
	a.resolveSlots = make(chan struct{}, 8)
	a.routes()
	return a, nil
}
func (a *App) Bootstrap() map[string]string {
	return map[string]string{"apiToken": a.token, "csrfToken": a.token}
}
func (a *App) Close() error          { a.media.Close(); a.catalog.Close(); return a.db.Close() }
func (a *App) Handler() http.Handler { return http.HandlerFunc(a.serve) }
func (a *App) secure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, network := range a.trusted {
		if network.Contains(ip) {
			return r.Header.Get("X-Forwarded-Proto") == "https"
		}
	}
	return false
}
func (a *App) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' https: data:; connect-src 'self'; media-src 'self' blob:; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("Cache-Control", "no-store")
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	if a.cfg.Mode == "desktop" && host != "127.0.0.1" && host != "localhost" && host != "::1" {
		failure(w, 403, "INVALID_HOST", "Invalid host")
		return
	}
	secure := a.secure(r)
	if a.cfg.Mode == "web" {
		remote, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(remote)
		if !secure && (ip == nil || !ip.IsLoopback()) {
			failure(w, 403, "TLS_REQUIRED", "HTTPS required")
			return
		}
		if secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	expected := scheme + "://" + r.Host
	if a.cfg.PublicOrigin != "" {
		if expected != a.cfg.PublicOrigin {
			failure(w, 403, "INVALID_HOST", "Invalid host")
			return
		}
		expected = a.cfg.PublicOrigin
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		same := origin == expected
		desktop := (a.cfg.Mode == "desktop" && (origin == "mygo://localhost" || origin == "http://mygo.localhost")) || (a.cfg.Development && a.cfg.DevOrigin != "" && origin == a.cfg.DevOrigin)
		if !same && !desktop {
			failure(w, 403, "ORIGIN_DENIED", "Origin denied")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	if a.cfg.Mode == "web" && r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && origin == "" {
		failure(w, 403, "ORIGIN_DENIED", "Origin required")
		return
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-CSRF-Token,Range,If-Range")
		w.WriteHeader(204)
		return
	}
	if values, ok := r.URL.Query()["limit"]; ok {
		if len(values) != 1 {
			failure(w, 400, "INVALID_INPUT", "limit must be provided once")
			return
		}
		n, e := strconv.Atoi(values[0])
		if e != nil || n < 1 || n > 50 {
			failure(w, 400, "INVALID_INPUT", "limit must be between 1 and 50")
			return
		}
	}
	a.mux.ServeHTTP(w, r)
}
func reply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "requestId": randomToken()[:12]})
}
func failure(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": msg, "retryable": status == 429 || status >= 502}, "requestId": randomToken()[:12]})
}
func handleErr(w http.ResponseWriter, err error) {
	var pe *provider.Error
	if errors.As(err, &pe) {
		status, code, msg := 502, "PROVIDER_UNAVAILABLE", "Source unavailable"
		switch pe.Code {
		case "timeout":
			status, code, msg = 504, "PROVIDER_TIMEOUT", "Source did not respond in time"
		case "rate_limited":
			status, code, msg = 429, "PROVIDER_RATE_LIMITED", "Source is rate limiting requests"
		case "auth":
			code, msg = "PROVIDER_AUTH", "Source authentication failed"
		case "forbidden":
			code, msg = "PROVIDER_FORBIDDEN", "Source access denied"
		case "malformed_source":
			code, msg = "MALFORMED_SOURCE", "Source returned an invalid response"
		case "invalid_mapping", "invalid_provider":
			status, code, msg = 400, "INVALID_INPUT", "Invalid source mapping"
		case "not_found":
			status, code, msg = 404, "NOT_FOUND", "Resource not found"
		case "unsupported":
			status, code, msg = 422, "UNSUPPORTED", "Unsupported option"
		}
		if pe.RetryAfter > 0 {
			seconds := int64((pe.RetryAfter + time.Second - 1) / time.Second)
			w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		}
		failure(w, status, code, msg)
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		failure(w, 404, "NOT_FOUND", "Resource not found")
	case errors.Is(err, store.ErrConflict):
		failure(w, 409, "CONFLICT", "Data changed. Reload before saving.")
	case errors.Is(err, store.ErrInvalid):
		failure(w, 400, "INVALID_INPUT", "Invalid input")
	case errors.Is(err, context.DeadlineExceeded):
		failure(w, 504, "PROVIDER_TIMEOUT", "Source did not respond in time")
	case errors.Is(err, context.Canceled):
		failure(w, 408, "REQUEST_CANCELED", "Request canceled")
	case errors.Is(err, media.ErrExpired):
		failure(w, 410, "SESSION_EXPIRED", "Playback session expired")
	case errors.Is(err, media.ErrInvalid):
		failure(w, 400, "INVALID_MEDIA", "Invalid media")
	case errors.Is(err, media.ErrLimit):
		failure(w, 429, "RATE_LIMITED", "Too many active media sessions")
	default:
		failure(w, 502, "PROVIDER_UNAVAILABLE", "Source or storage unreachable")
	}
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		failure(w, 400, "INVALID_INPUT", "Invalid JSON")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		failure(w, 400, "INVALID_INPUT", "Unexpected trailing data in body")
		return false
	}
	return true
}
func (a *App) identity(r *http.Request) (principal, bool) {
	// Every API call, desktop included, needs a Google-backed session. The web
	// app carries it in a cookie; the desktop webview runs on mygo:// and cannot
	// share that cookie, so it sends the session token as a bearer credential.
	// The bootstrap capability is never a session: it only authorizes starting
	// and claiming the sign-in flow.
	token := ""
	if a.cfg.Mode == "desktop" {
		value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || constantTimeEqual(value, a.token) {
			return principal{}, false
		}
		token = value
	} else {
		c, e := r.Cookie("myanime_session")
		if e != nil {
			return principal{}, false
		}
		token = c.Value
	}
	a.mu.Lock()
	s, ok := a.sessions[token]
	if !ok {
		a.mu.Unlock()
		return principal{}, false
	}
	now := time.Now()
	if now.Sub(s.last) > 24*time.Hour || now.Sub(s.created) > 7*24*time.Hour {
		delete(a.sessions, token)
		a.mu.Unlock()
		a.media.RevokeOwner(token)
		return principal{}, false
	}
	s.last = now
	a.sessions[token] = s
	a.mu.Unlock()
	return principal{owner: token, csrf: s.csrf, email: s.email}, true
}
func (a *App) protect(fn func(http.ResponseWriter, *http.Request, principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.identity(r)
		if !ok {
			failure(w, 401, "UNAUTHORIZED", "Sign in required")
			return
		}
		// Both the cookie session and the desktop bearer session carry a CSRF
		// token; a mutation must prove it.
		if r.Method != "GET" && r.Method != "HEAD" && !constantTimeEqual(r.Header.Get("X-CSRF-Token"), p.csrf) {
			failure(w, 403, "CSRF_DENIED", "Invalid session")
			return
		}
		fn(w, r, p)
	}
}

// public serves read-only catalogue data without a session. Playback, bookmarks,
// history, progress and settings stay behind protect: browsing is open, watching
// is not.
func (a *App) public(fn func(http.ResponseWriter, *http.Request, principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			failure(w, 405, "DENIED", "Method not allowed")
			return
		}
		p, _ := a.identity(r)
		fn(w, r, p)
	}
}
func pageArgs(r *http.Request) (int, string) {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 {
		n = 24
	}
	if n > 50 {
		n = 50
	}
	return n, r.URL.Query().Get("cursor")
}
func (a *App) routes() {
	m := a.mux
	m.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]string{"status": "ok"}) })
	m.HandleFunc("GET /api/v1/auth/session", a.sessionInfo)
	m.HandleFunc("GET /api/v1/auth/google/start", a.googleStart)
	m.HandleFunc("GET /api/v1/auth/google/callback", a.googleCallback)
	m.HandleFunc("POST /api/v1/auth/google/claim", a.desktopClaim)
	m.HandleFunc("POST /api/v1/auth/logout", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		a.mu.Lock()
		delete(a.sessions, p.owner)
		a.mu.Unlock()
		a.media.RevokeOwner(p.owner)
		http.SetCookie(w, &http.Cookie{Name: "myanime_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure(r), SameSite: http.SameSiteLaxMode})
		w.WriteHeader(204)
	}))
	m.HandleFunc("GET /api/v1/capabilities", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		reply(w, 200, map[string]any{"desktop": a.cfg.Mode == "desktop", "csrfToken": p.csrf, "providers": a.catalog.Providers()})
	}))
	m.HandleFunc("GET /api/v1/providers", a.public(func(w http.ResponseWriter, r *http.Request, p principal) {
		reply(w, 200, map[string]any{"items": a.catalog.Providers()})
	}))
	m.HandleFunc("GET /api/v1/catalog", a.public(func(w http.ResponseWriter, r *http.Request, p principal) {
		n, c := pageArgs(r)
		q := r.URL.Query()
		year, _ := strconv.Atoi(q.Get("year"))
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		result, e := a.catalog.Search(ctx, domain.CatalogQuery{Query: q.Get("query"), Cursor: c, Limit: n, Genre: q.Get("genre"), Season: q.Get("season"), Year: year})
		if e != nil {
			handleErr(w, e)
			return
		}
		for _, anime := range result.Items {
			if e = a.db.UpsertAnime(ctx, anime); e != nil {
				handleErr(w, e)
				return
			}
		}
		reply(w, 200, result)
	}))
	m.HandleFunc("GET /api/v1/anime/{id}", a.public(func(w http.ResponseWriter, r *http.Request, p principal) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		v, e := a.catalog.Detail(ctx, r.PathValue("id"))
		if e != nil {
			handleErr(w, e)
			return
		}
		if e = a.db.UpsertAnime(ctx, v); e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, v)
	}))
	m.HandleFunc("GET /api/v1/anime/{id}/episodes", a.public(func(w http.ResponseWriter, r *http.Request, p principal) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		id := r.PathValue("id")
		anime, e := a.catalog.Detail(ctx, id)
		if e != nil {
			handleErr(w, e)
			return
		}
		if e = a.db.UpsertAnime(ctx, anime); e != nil {
			handleErr(w, e)
			return
		}
		v, e := a.catalog.Episodes(ctx, id)
		if e != nil {
			handleErr(w, e)
			return
		}
		if e = a.db.UpsertEpisodes(ctx, v); e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, map[string]any{"items": v, "nextCursor": nil})
	}))
	m.HandleFunc("POST /api/v1/playback/sessions", a.protect(a.resolve))
	for _, kind := range []string{"bookmarks", "cache"} {
		kind := kind
		m.HandleFunc("DELETE /api/v1/"+kind, a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
			if r.URL.Query().Get("confirmed") != "true" {
				failure(w, 400, "CONFIRM_REQUIRED", "Confirmation required")
				return
			}
			var err error
			if kind == "bookmarks" {
				err = a.db.ClearBookmarks(r.Context())
			} else {
				err = a.db.ClearCache(r.Context())
				if err == nil {
					a.catalog.Refresh()
				}
			}
			if err != nil {
				handleErr(w, err)
				return
			}
			w.WriteHeader(204)
		}))
	}
	m.HandleFunc("DELETE /api/v1/playback/sessions/{id}", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		a.media.Revoke(r.PathValue("id"), p.owner)
		w.WriteHeader(204)
	}))
	m.HandleFunc("POST /api/v1/playback/sessions/{id}/subtitles", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		r.Body = http.MaxBytesReader(w, r.Body, (2<<20)+8192)
		if e := r.ParseMultipartForm(2 << 20); e != nil {
			failure(w, 400, "INVALID_SUBTITLE", "Subtitle too large or invalid")
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, h, e := r.FormFile("file")
		if e != nil {
			failure(w, 400, "INVALID_SUBTITLE", "File required")
			return
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, (2<<20)+1))
		if e != nil || len(b) > 2<<20 {
			failure(w, 400, "INVALID_SUBTITLE", "Subtitle too large")
			return
		}
		v, e := a.media.AddSubtitle(r.PathValue("id"), p.owner, h.Filename, b)
		if e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 201, v)
	}))
	m.Handle("/media/", a.media.Handler())
	m.HandleFunc("GET /api/v1/bookmarks", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		n, c := pageArgs(r)
		v, e := a.db.Bookmarks(r.Context(), n, c)
		if e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, v)
	}))
	m.HandleFunc("PUT /api/v1/bookmarks/{id}", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		id := r.PathValue("id")
		if _, e := a.db.Anime(r.Context(), id); e != nil {
			v, e := a.catalog.Detail(r.Context(), id)
			if e != nil {
				handleErr(w, e)
				return
			}
			if e = a.db.UpsertAnime(r.Context(), v); e != nil {
				handleErr(w, e)
				return
			}
		}
		if e := a.db.PutBookmark(r.Context(), id); e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, map[string]bool{"bookmarked": true})
	}))
	m.HandleFunc("DELETE /api/v1/bookmarks/{id}", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		if e := a.db.DeleteBookmark(r.Context(), r.PathValue("id")); e != nil {
			handleErr(w, e)
			return
		}
		w.WriteHeader(204)
	}))
	m.HandleFunc("GET /api/v1/history", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		n, c := pageArgs(r)
		v, e := a.db.History(r.Context(), n, c)
		if e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, v)
	}))
	m.HandleFunc("DELETE /api/v1/history", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		if r.URL.Query().Get("confirmed") != "true" {
			failure(w, 400, "CONFIRM_REQUIRED", "Confirmation required")
			return
		}
		if e := a.db.ClearHistory(r.Context()); e != nil {
			handleErr(w, e)
			return
		}
		w.WriteHeader(204)
	}))
	m.HandleFunc("GET /api/v1/progress/{id}", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		v, e := a.db.Progress(r.Context(), r.PathValue("id"))
		if errors.Is(e, store.ErrNotFound) {
			reply(w, 200, nil)
			return
		}
		if e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, v)
	}))
	m.HandleFunc("PUT /api/v1/progress/{id}", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		var b store.ProgressUpdate
		if !decode(w, r, &b) {
			return
		}
		v, e := a.db.PutProgress(r.Context(), r.PathValue("id"), b)
		if e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, v)
	}))
	m.HandleFunc("GET /api/v1/settings", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		v, e := a.db.Settings(r.Context())
		if e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, v)
	}))
	m.HandleFunc("PATCH /api/v1/settings", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		var b map[string]json.RawMessage
		if !decode(w, r, &b) {
			return
		}
		var rev int64
		if e := json.Unmarshal(b["revision"], &rev); e != nil {
			failure(w, 400, "INVALID_REVISION", "revision is required")
			return
		}
		delete(b, "revision")
		v, e := a.db.PatchSettings(r.Context(), rev, b)
		if e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, v)
	}))
	m.HandleFunc("POST /api/v1/settings/reset", a.protect(func(w http.ResponseWriter, r *http.Request, p principal) {
		var b struct {
			Revision int64 `json:"revision"`
		}
		if !decode(w, r, &b) {
			return
		}
		v, e := a.db.ResetSettings(r.Context(), b.Revision)
		if e != nil {
			handleErr(w, e)
			return
		}
		reply(w, 200, v)
	}))
	for _, name := range []string{"genres", "schedule"} {
		name := name
		m.HandleFunc("GET /api/v1/"+name, a.public(func(w http.ResponseWriter, r *http.Request, p principal) {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			var v any
			var e error
			if name == "genres" {
				if s, ok := any(a.catalog).(interface {
					Genres(context.Context) (any, error)
				}); ok {
					v, e = s.Genres(ctx)
				} else {
					e = errors.New("genres unavailable")
				}
			} else {
				if s, ok := any(a.catalog).(interface {
					Schedule(context.Context) (any, error)
				}); ok {
					v, e = s.Schedule(ctx)
				} else {
					e = errors.New("schedule unavailable")
				}
			}
			if e != nil {
				handleErr(w, e)
				return
			}
			reply(w, 200, v)
		}))
	}
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { failure(w, 404, "NOT_FOUND", "Endpoint not found") })
	m.HandleFunc("/", a.frontend)
}
func (a *App) resolve(w http.ResponseWriter, r *http.Request, p principal) {
	var b domain.ResolveRequest
	if !decode(w, r, &b) {
		return
	}
	if b.AnimeID == "" || b.EpisodeID == "" || (b.Mode != "sub" && b.Mode != "dub") {
		failure(w, 400, "INVALID_INPUT", "animeId, episodeId and mode are required")
		return
	}
	select {
	case a.resolveSlots <- struct{}{}:
		defer func() { <-a.resolveSlots }()
	default:
		w.Header().Set("Retry-After", "1")
		failure(w, 429, "RATE_LIMITED", "Too many concurrent resolutions")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	s, e := a.catalog.Resolve(ctx, b)
	if e != nil {
		handleErr(w, e)
		return
	}
	scheme := "http"
	if a.secure(r) {
		scheme = "https"
	}
	a.mu.Lock()
	v, e := a.media.Create(ctx, s, p.owner, scheme+"://"+r.Host)
	if e != nil {
		handleErr(w, e)
		return
	}
	reply(w, 201, v)
}
func (a *App) frontend(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if a.cfg.FrontendFS != nil {
		path := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+r.URL.Path)), "/")
		if st, e := fs.Stat(a.cfg.FrontendFS, path); e != nil || st.IsDir() {
			r = r.Clone(r.Context())
			u := *r.URL
			u.Path = "/"
			r.URL = &u
		}
		http.FileServer(http.FS(a.cfg.FrontendFS)).ServeHTTP(w, r)
		return
	}
	dir := a.cfg.FrontendDir
	if dir == "" {
		dir = "web/dist"
	}
	path := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
	if st, e := os.Stat(path); e == nil && !st.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	index := filepath.Join(dir, "index.html")
	if _, e := os.Stat(index); e != nil {
		http.Error(w, fmt.Sprintf("Frontend belum tersedia di %s. Jalankan Vite dev server atau build frontend dengan persetujuan pemilik.", dir), 503)
		return
	}
	http.ServeFile(w, r, index)
}
