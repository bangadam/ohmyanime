package app

import (
	"context"
	"encoding/json"
	"myanime/internal/domain"
	"myanime/internal/provider"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testGoogle() GoogleConfig {
	return GoogleConfig{
		ClientID:            "web-client",
		ClientSecret:        "web-secret",
		DesktopClientID:     "desktop-client",
		DesktopClientSecret: "desktop-secret",
		AllowedEmails:       []string{"adam@example.com"},
	}
}

func authApp(t *testing.T, mode string) *App {
	t.Helper()
	a, e := New(Config{Mode: mode, DataDir: t.TempDir(), Google: testGoogle()})
	if e != nil {
		t.Fatal(e)
	}
	// Exchange and verification are replaced so the flow can be exercised without
	// contacting Google; the callback still runs its own state, PKCE and allowlist
	// checks.
	a.exchange = func(context.Context, string, string, string, string, string) (string, error) {
		return "stub-id-token", nil
	}
	a.verify = func(_ context.Context, raw, audience string) (googleIdentity, error) {
		if raw != "stub-id-token" || (audience != "web-client" && audience != "desktop-client") {
			return googleIdentity{}, context.Canceled
		}
		return googleIdentity{Subject: "sub-1", Email: "adam@example.com", EmailVerified: true}, nil
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func authRequest(a *App, method, path, body, origin string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://localhost"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

// startFlow performs the browser half of the flow and returns the session cookie
// plus CSRF token, the way a real redirect back to the app would.
func startFlow(t *testing.T, a *App, old *http.Cookie) (*http.Cookie, string) {
	t.Helper()
	w := authRequest(a, "GET", "/api/v1/auth/google/start", "", "", nil, "")
	if w.Code != http.StatusFound {
		t.Fatalf("start %d: %s", w.Code, w.Body.String())
	}
	location, e := url.Parse(w.Header().Get("Location"))
	if e != nil {
		t.Fatal(e)
	}
	if location.Host != "accounts.google.com" {
		t.Fatalf("unexpected provider %q", location.Host)
	}
	state := location.Query().Get("state")
	challenge := location.Query().Get("code_challenge")
	if state == "" || challenge == "" || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("missing PKCE/state: %s", location.RawQuery)
	}
	w = authRequest(a, "GET", "/api/v1/auth/google/callback?state="+url.QueryEscape(state)+"&code=abc", "", "", old, "")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("callback %d: %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	c := cookies[0]
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe cookie")
	}
	session := authRequest(a, "GET", "/api/v1/auth/session", "", "", c, "")
	var payload struct {
		Data struct {
			Authenticated bool   `json:"authenticated"`
			CSRF          string `json:"csrfToken"`
			Email         string `json:"email"`
		}
	}
	if e := json.Unmarshal(session.Body.Bytes(), &payload); e != nil {
		t.Fatal(e)
	}
	if !payload.Data.Authenticated || payload.Data.CSRF == "" || payload.Data.Email != "adam@example.com" {
		t.Fatalf("unexpected session: %s", session.Body.String())
	}
	return c, payload.Data.CSRF
}

func TestGoogleFlowCSRFAndLogout(t *testing.T) {
	a := authApp(t, "web")
	cookie, csrf := startFlow(t, a, nil)
	session, e := a.media.Create(context.Background(), domain.Source{MasterURL: "https://cdn.example/master.m3u8"}, cookie.Value, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, origin := range []string{"", "null", "https://evil.example"} {
		w := authRequest(a, "POST", "/api/v1/auth/logout", "", origin, cookie, csrf)
		if w.Code != 403 {
			t.Fatalf("origin %q: %d", origin, w.Code)
		}
	}
	if w := authRequest(a, "POST", "/api/v1/auth/logout", "", "https://localhost", cookie, ""); w.Code != 403 {
		t.Fatal("csrf not enforced")
	}
	next, nextCSRF := startFlow(t, a, cookie)
	if next.Value == cookie.Value || nextCSRF == csrf {
		t.Fatal("session not rotated")
	}
	if w := authRequest(a, "GET", "/api/v1/bookmarks", "", "", cookie, ""); w.Code != 401 {
		t.Fatal("old cookie accepted")
	}
	if _, e = a.media.AddSubtitle(session.ID, cookie.Value, "test.vtt", []byte("WEBVTT\n\n")); e == nil {
		t.Fatal("old media survived rotation")
	}
	if w := authRequest(a, "POST", "/api/v1/auth/logout", "", "https://localhost", next, nextCSRF); w.Code != 204 {
		t.Fatal(w.Code)
	}
	if w := authRequest(a, "GET", "/api/v1/bookmarks", "", "", next, ""); w.Code != 401 {
		t.Fatal("revoked cookie accepted")
	}
}

func TestGoogleFlowRejectsUnusableCallbacks(t *testing.T) {
	a := authApp(t, "web")
	// Unknown state, cancelled consent, and a disallowed account all fail closed.
	if w := authRequest(a, "GET", "/api/v1/auth/google/callback?state=unknown&code=abc", "", "", nil, ""); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "auth=expired") {
		t.Fatalf("unknown state: %d %s", w.Code, w.Header().Get("Location"))
	}
	w := authRequest(a, "GET", "/api/v1/auth/google/start?return=/search", "", "", nil, "")
	state, _ := url.Parse(w.Header().Get("Location"))
	value := state.Query().Get("state")
	denied := authRequest(a, "GET", "/api/v1/auth/google/callback?state="+url.QueryEscape(value)+"&error=access_denied", "", "", nil, "")
	if denied.Code != http.StatusSeeOther || !strings.Contains(denied.Header().Get("Location"), "/search?auth=denied") {
		t.Fatalf("denied: %d %s", denied.Code, denied.Header().Get("Location"))
	}
	w = authRequest(a, "GET", "/api/v1/auth/google/start", "", "", nil, "")
	state, _ = url.Parse(w.Header().Get("Location"))
	a.verify = func(context.Context, string, string) (googleIdentity, error) {
		return googleIdentity{Subject: "sub-2", Email: "stranger@example.com", EmailVerified: true}, nil
	}
	notAllowed := authRequest(a, "GET", "/api/v1/auth/google/callback?state="+url.QueryEscape(state.Query().Get("state"))+"&code=abc", "", "", nil, "")
	if notAllowed.Code != http.StatusSeeOther || !strings.Contains(notAllowed.Header().Get("Location"), "auth=not_allowed") {
		t.Fatalf("allowlist: %d %s", notAllowed.Code, notAllowed.Header().Get("Location"))
	}
	// The state is single-use: replaying it cannot mint a second session.
	replay := authRequest(a, "GET", "/api/v1/auth/google/callback?state="+url.QueryEscape(state.Query().Get("state"))+"&code=abc", "", "", nil, "")
	if !strings.Contains(replay.Header().Get("Location"), "auth=expired") {
		t.Fatalf("state replayed: %s", replay.Header().Get("Location"))
	}
}

func TestGoogleStartRefusesRedirects(t *testing.T) {
	a := authApp(t, "web")
	w := authRequest(a, "GET", "/api/v1/auth/google/start?return=//evil.example/x", "", "", nil, "")
	state, _ := url.Parse(w.Header().Get("Location"))
	flow := a.flow(state.Query().Get("state"))
	if flow == nil || flow.returnTo != "/" {
		t.Fatalf("protocol-relative return accepted: %+v", flow)
	}
	if w := authRequest(a, "GET", "/api/v1/auth/google/start?return="+url.QueryEscape("https://evil.example"), "", "", nil, ""); w.Code != http.StatusFound {
		t.Fatal(w.Code)
	}
}

func TestGoogleAllowlistRules(t *testing.T) {
	a := authApp(t, "web")
	for _, tc := range []struct {
		email string
		want  bool
	}{{"adam@example.com", true}, {"Adam@Example.com", true}, {"other@example.com", false}, {"", false}, {"no-at-sign", false}} {
		if got := a.googleAllowed(tc.email); got != tc.want {
			t.Fatalf("%q: %v", tc.email, got)
		}
	}
	a.cfg.Google.AllowedEmails = nil
	a.cfg.Google.AllowedDomains = []string{"Example.com"}
	if !a.googleAllowed("anyone@example.com") || a.googleAllowed("anyone@evil.example") {
		t.Fatal("domain allowlist broken")
	}
	a.cfg.Google.AllowedEmails = nil
	a.cfg.Google.AllowedDomains = nil
	if a.googleAllowed("adam@example.com") {
		t.Fatal("empty allowlist must deny")
	}
	if a.googleConfigured() {
		t.Fatal("empty allowlist must not report a usable configuration")
	}
}

func TestGoogleNotConfigured(t *testing.T) {
	a, e := New(Config{Mode: "web", DataDir: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.Close() })
	if w := authRequest(a, "GET", "/api/v1/auth/google/start", "", "", nil, ""); w.Code != 503 {
		t.Fatalf("start without configuration: %d", w.Code)
	}
	session := authRequest(a, "GET", "/api/v1/auth/session", "", "", nil, "")
	if !strings.Contains(session.Body.String(), `"configured":false`) {
		t.Fatalf("session: %s", session.Body.String())
	}
	if w := authRequest(a, "GET", "/api/v1/providers", "", "", nil, ""); w.Code != 200 {
		t.Fatal("public catalogue read denied")
	}
	if w := authRequest(a, "POST", "/api/v1/playback/sessions", `{"animeId":"a","episodeId":"b","mode":"sub"}`, "https://localhost", nil, ""); w.Code != 401 {
		t.Fatal("playback without a session accepted")
	}
}

func TestDesktopClaimDeliversSessionOnce(t *testing.T) {
	a := authApp(t, "desktop")
	r := httptest.NewRequest("GET", "http://127.0.0.1:1/api/v1/auth/google/start", nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("desktop start %d: %s", w.Code, w.Body.String())
	}
	var start struct {
		Data struct {
			URL       string `json:"url"`
			Handoff   string `json:"handoff"`
			PollToken string `json:"pollToken"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &start); e != nil {
		t.Fatal(e)
	}
	if start.Data.URL == "" || start.Data.Handoff == "" || start.Data.PollToken == "" {
		t.Fatalf("incomplete start: %s", w.Body.String())
	}
	state, _ := url.Parse(start.Data.URL)
	// The shell opens the URL in the system browser; the callback then lands here.
	callback := authRequest(a, "GET", "/api/v1/auth/google/callback?state="+url.QueryEscape(state.Query().Get("state"))+"&code=abc", "", "", nil, "")
	if callback.Code != 200 || !strings.Contains(callback.Body.String(), "Signed in") {
		t.Fatalf("desktop callback %d", callback.Code)
	}
	claim := func(poll string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:1/api/v1/auth/google/claim", strings.NewReader(`{"handoff":"`+start.Data.Handoff+`","pollToken":"`+poll+`"}`))
		r.Header.Set("Authorization", "Bearer "+a.token)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, r)
		return rec
	}
	if w := claim("wrong"); w.Code != 400 {
		t.Fatalf("wrong poll token accepted: %d", w.Code)
	}
	w = claim(start.Data.PollToken)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"authenticated":true`) {
		t.Fatalf("claim %d: %s", w.Code, w.Body.String())
	}
	if w := claim(start.Data.PollToken); w.Code != 400 {
		t.Fatal("session delivered twice")
	}
}

func TestDesktopNeedsASessionNotJustCapability(t *testing.T) {
	a := authApp(t, "desktop")
	r := httptest.NewRequest("GET", "http://127.0.0.1:1/api/v1/providers", nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("public read denied: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "http://evil.example/api/v1/providers", nil)
	r.Header.Set("Authorization", "Bearer "+a.token)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("forged host accepted")
	}
	session, csrf, err := a.newSession("adam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1:1/api/v1/providers", nil)
	r.Header.Set("Authorization", "Bearer "+session)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("session read %d", w.Code)
	}
	r = httptest.NewRequest("POST", "http://127.0.0.1:1/api/v1/auth/logout", nil)
	r.Header.Set("Authorization", "Bearer "+session)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("desktop mutation without csrf: %d", w.Code)
	}
	r.Header.Set("X-CSRF-Token", csrf)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("desktop mutation with csrf: %d", w.Code)
	}
}

func TestAuthExpiry(t *testing.T) {
	a := authApp(t, "web")
	for _, tc := range []struct {
		name          string
		created, last time.Time
	}{{"idle", time.Now().Add(-25 * time.Hour), time.Now().Add(-25 * time.Hour)}, {"hard", time.Now().Add(-8 * 24 * time.Hour), time.Now()}} {
		t.Run(tc.name, func(t *testing.T) {
			a.sessions[tc.name] = loginSession{csrf: "csrf", email: "adam@example.com", created: tc.created, last: tc.last}
			w := authRequest(a, "GET", "/api/v1/bookmarks", "", "", &http.Cookie{Name: "myanime_session", Value: tc.name}, "")
			if w.Code != 401 {
				t.Fatal(w.Code)
			}
			if _, ok := a.sessions[tc.name]; ok {
				t.Fatal("expired session retained")
			}
		})
	}
}

func TestForwardedTLSAndDevelopmentOrigins(t *testing.T) {
	a := authApp(t, "web")
	r := httptest.NewRequest("GET", "http://localhost/api/v1/auth/session", nil)
	r.RemoteAddr = "203.0.113.1:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted proxy accepted")
	}
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Origin", "http://localhost:5173")
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("production dev origin accepted")
	}
	a.cfg.Development = true
	a.cfg.DevOrigin = "http://localhost:5173"
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != a.cfg.DevOrigin {
		t.Fatal("development origin rejected")
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
}

func TestProviderTypedErrors(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{{"timeout", 504}, {"rate_limited", 429}, {"invalid_mapping", 400}, {"forbidden", 502}} {
		w := httptest.NewRecorder()
		handleErr(w, &provider.Error{Code: tc.code, Provider: "https://secret.example/token", RetryAfter: 1500 * time.Millisecond})
		if w.Code != tc.status || strings.Contains(w.Body.String(), "secret.example") {
			t.Fatal(w.Code, w.Body.String())
		}
		if w.Header().Get("Retry-After") != "2" {
			t.Fatal("Retry-After not rounded")
		}
	}
}
