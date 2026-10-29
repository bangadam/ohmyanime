package app

import (
	"net/url"
	"testing"
)

// The redirect URI must point at the origin the browser actually uses. In
// development the browser talks to Vite, which proxies to the API with its own
// Host, so the dev origin has to win.
func TestGoogleRedirectUsesBrowserOrigin(t *testing.T) {
	a := authApp(t, "web")
	a.cfg.Development = true
	a.cfg.DevOrigin = "http://127.0.0.1:5173"
	w := authRequest(a, "GET", "/api/v1/auth/google/start", "", "", nil, "")
	location, e := url.Parse(w.Header().Get("Location"))
	if e != nil {
		t.Fatal(e)
	}
	if got := location.Query().Get("redirect_uri"); got != "http://127.0.0.1:5173/api/v1/auth/google/callback" {
		t.Fatalf("redirect_uri %q", got)
	}
	state := a.flow(location.Query().Get("state"))
	if state == nil || state.redirect != "http://127.0.0.1:5173/api/v1/auth/google/callback" {
		t.Fatalf("flow did not keep the redirect URI: %+v", state)
	}
	a.cfg.Development = false
	w = authRequest(a, "GET", "/api/v1/auth/google/start", "", "", nil, "")
	location, _ = url.Parse(w.Header().Get("Location"))
	if got := location.Query().Get("redirect_uri"); got != "https://localhost/api/v1/auth/google/callback" {
		t.Fatalf("production redirect_uri %q", got)
	}
}
