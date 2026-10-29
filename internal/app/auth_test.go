package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"myanime/internal/domain"
	"myanime/internal/provider"
)

func authApp(t *testing.T, mode string) *App {
	t.Helper()
	a, err := New(Config{Mode: mode, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func capabilityRequest(a *App, method, path, body, origin, bearer, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://localhost"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestWebCapabilitiesWithoutLogin(t *testing.T) {
	a := authApp(t, "web")
	w := capabilityRequest(a, "GET", "/api/v1/capabilities", "", "", "", "")
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("capabilities: %d %s", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			Desktop   bool              `json:"desktop"`
			CSRF      string            `json:"csrfToken"`
			Providers []json.RawMessage `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Desktop || result.Data.CSRF == "" || len(result.Data.Providers) == 0 {
		t.Fatalf("invalid capabilities: %s", w.Body.String())
	}
	for _, path := range []string{"/settings", "/bookmarks", "/history", "/progress/missing", "/providers"} {
		if w := capabilityRequest(a, "GET", "/api/v1"+path, "", "", "", ""); w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, csrf := range []string{"", "wrong"} {
		if w := capabilityRequest(a, "PATCH", "/api/v1/settings", `{"revision":0,"autoplay":true}`, "https://localhost", "", csrf); w.Code != 403 {
			t.Fatalf("invalid CSRF accepted: %d", w.Code)
		}
	}
	if w := capabilityRequest(a, "PATCH", "/api/v1/settings", `{"revision":0,"autoplay":true}`, "https://localhost", "", result.Data.CSRF); w.Code != 200 {
		t.Fatalf("no-login mutation: %d %s", w.Code, w.Body.String())
	}
	for _, origin := range []string{"", "https://evil.example"} {
		if w := capabilityRequest(a, "DELETE", "/api/v1/history?confirmed=true", "", origin, "", result.Data.CSRF); w.Code != 403 {
			t.Fatalf("invalid origin accepted: %d", w.Code)
		}
	}
}

func TestDesktopBootstrapCapability(t *testing.T) {
	a := authApp(t, "desktop")
	credentials := a.Bootstrap()
	for _, bearer := range []string{"", "wrong"} {
		for _, path := range []string{"/capabilities", "/providers", "/settings", "/bookmarks"} {
			if w := capabilityRequest(a, "GET", "/api/v1"+path, "", "", bearer, ""); w.Code != 401 {
				t.Fatalf("%s without capability: %d", path, w.Code)
			}
		}
	}
	w := capabilityRequest(a, "GET", "/api/v1/capabilities", "", "mygo://localhost", credentials["apiToken"], "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"desktop":true`) || !strings.Contains(w.Body.String(), credentials["csrfToken"]) {
		t.Fatalf("bootstrap rejected: %d %s", w.Code, w.Body.String())
	}
	if w := capabilityRequest(a, "DELETE", "/api/v1/history?confirmed=true", "", "mygo://localhost", credentials["apiToken"], ""); w.Code != 403 {
		t.Fatal("desktop mutation without CSRF accepted")
	}
	if w := capabilityRequest(a, "DELETE", "/api/v1/history?confirmed=true", "", "mygo://localhost", credentials["apiToken"], credentials["csrfToken"]); w.Code != 204 {
		t.Fatalf("desktop mutation: %d %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", "http://evil.example/api/v1/capabilities", nil)
	r.Header.Set("Authorization", "Bearer "+credentials["apiToken"])
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("desktop foreign host accepted")
	}
}

func TestRemovedAuthEndpoints(t *testing.T) {
	for _, mode := range []string{"web", "desktop"} {
		a := authApp(t, mode)
		for _, endpoint := range []struct{ method, path string }{
			{"GET", "/session"}, {"GET", "/google/start"}, {"GET", "/google/callback"},
			{"POST", "/google/claim"}, {"POST", "/logout"},
		} {
			w := capabilityRequest(a, endpoint.method, "/api/v1/auth"+endpoint.path, "", "https://localhost", a.token, a.token)
			if w.Code != 404 || len(w.Result().Cookies()) != 0 {
				t.Fatalf("%s %s still available: %d", mode, endpoint.path, w.Code)
			}
		}
	}
}

func TestInstanceMediaOwnership(t *testing.T) {
	for _, mode := range []string{"web", "desktop"} {
		a := authApp(t, mode)
		r := httptest.NewRequest("GET", "https://localhost/api/v1/capabilities", nil)
		r.Header.Set("Authorization", "Bearer "+a.token)
		p, ok := a.identity(r)
		if !ok || p.owner != a.token {
			t.Fatal("missing instance owner")
		}
		session, err := a.media.Create(context.Background(), domain.Source{MasterURL: "https://cdn.example/master.m3u8"}, p.owner, "")
		if err != nil {
			t.Fatal(err)
		}
		if a.media.Revoke(session.ID, "other-instance") {
			t.Fatal("foreign instance revoked media")
		}
		if !a.media.Revoke(session.ID, p.owner) {
			t.Fatal("instance could not revoke its media")
		}
	}
}

func TestNoLoginPreservesExistingDatabase(t *testing.T) {
	dataDir := t.TempDir()
	a, err := New(Config{Mode: "web", DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	w := capabilityRequest(a, "PATCH", "/api/v1/settings", `{"revision":0,"quality":"720p"}`, "https://localhost", "", a.token)
	if w.Code != 200 {
		_ = a.Close()
		t.Fatalf("save settings: %d %s", w.Code, w.Body.String())
	}
	oldToken := a.token
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a, err = New(Config{Mode: "web", DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.token == oldToken {
		t.Fatal("instance capability survived restart")
	}
	w = capabilityRequest(a, "GET", "/api/v1/settings", "", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"quality":"720p"`) {
		t.Fatalf("existing settings lost: %d %s", w.Code, w.Body.String())
	}
	w = capabilityRequest(a, "DELETE", "/api/v1/history?confirmed=true", "", "https://localhost", "", oldToken)
	if w.Code != 403 {
		t.Fatal("previous instance CSRF accepted")
	}
}

func TestForwardedTLSAndDevelopmentOrigins(t *testing.T) {
	a := authApp(t, "web")
	r := httptest.NewRequest("GET", "http://localhost/api/v1/capabilities", nil)
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
