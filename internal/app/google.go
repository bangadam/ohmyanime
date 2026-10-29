package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

const (
	googleScope   = "openid email profile"
	flowTTL       = 10 * time.Minute
	maxFlows      = 256
	claimPending  = 202
	claimAccepted = 200
)

// googleIdentity is the only identity this application accepts.
type googleIdentity struct {
	Subject       string
	Email         string
	EmailVerified bool
}

type oauthFlow struct {
	mode     string
	verifier string
	redirect string
	returnTo string
	handoff  string
	poll     string
	created  time.Time
	session  string
	email    string
	failure  string
}

// exchangeCode redeems an authorization code for an ID token. It is a field so
// tests can exercise the callback without contacting Google.
func exchangeCode(ctx context.Context, clientID, clientSecret, redirectURI, code, verifier string) (string, error) {
	cfg := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURI,
		Scopes:       strings.Fields(googleScope),
	}
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", err
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return "", errors.New("google did not return an ID token")
	}
	return raw, nil
}

func verifyGoogleIDToken(ctx context.Context, raw, audience string) (googleIdentity, error) {
	validator, err := idtoken.NewValidator(ctx)
	if err != nil {
		return googleIdentity{}, err
	}
	payload, err := validator.Validate(ctx, raw, audience)
	if err != nil {
		return googleIdentity{}, err
	}
	email, _ := payload.Claims["email"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	if email == "" {
		return googleIdentity{}, errors.New("google account has no email")
	}
	return googleIdentity{Subject: payload.Subject, Email: strings.ToLower(email), EmailVerified: verified}, nil
}

func (a *App) googleConfigured() bool {
	g := a.cfg.Google
	return g.ClientID != "" && g.ClientSecret != "" && (len(g.AllowedEmails) > 0 || len(g.AllowedDomains) > 0)
}

// clientFor returns the OAuth client for a mode. A desktop shell cannot reuse
// the web client: Google only accepts a loopback redirect for an installed-app
// client, and the shell's port is ephemeral, so the two clients never mix.
func (a *App) clientFor(mode string) (id, secret string, ok bool) {
	g := a.cfg.Google
	if mode == "desktop" {
		return g.DesktopClientID, g.DesktopClientSecret, g.DesktopClientID != "" && g.DesktopClientSecret != ""
	}
	return g.ClientID, g.ClientSecret, g.ClientID != "" && g.ClientSecret != ""
}

// googleAllowed applies the allowlist. An empty allowlist denies every account:
// this is a single-user instance and the check must fail closed.
func (a *App) googleAllowed(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	for _, allowed := range a.cfg.Google.AllowedEmails {
		if strings.EqualFold(strings.TrimSpace(allowed), email) {
			return true
		}
	}
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return false
	}
	domain := email[at+1:]
	for _, allowed := range a.cfg.Google.AllowedDomains {
		allowed = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(allowed), "@"))
		if allowed != "" && domain == allowed {
			return true
		}
	}
	return false
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// localPath accepts only an application path, never an absolute or
// protocol-relative URL, so the callback cannot be used as an open redirect.
func localPath(value string) string {
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "\\\r\n") {
		return "/"
	}
	return value
}

func (a *App) pruneFlows(now time.Time) {
	for key, flow := range a.flows {
		if now.Sub(flow.created) > flowTTL {
			delete(a.flows, key)
		}
	}
}

func (a *App) putFlow(key string, flow *oauthFlow) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneFlows(time.Now())
	if len(a.flows) >= maxFlows {
		for k := range a.flows {
			delete(a.flows, k)
			break
		}
	}
	a.flows[key] = flow
}

func (a *App) flow(key string) *oauthFlow {
	a.mu.Lock()
	defer a.mu.Unlock()
	flow, ok := a.flows[key]
	if !ok || time.Since(flow.created) > flowTTL {
		delete(a.flows, key)
		return nil
	}
	return flow
}

func (a *App) dropFlow(key string) {
	a.mu.Lock()
	delete(a.flows, key)
	a.mu.Unlock()
}

// flowByHandoff finds the desktop attempt a shell is polling for. The state is
// the callback key, so the handoff token needs its own lookup.
func (a *App) flowByHandoff(handoff string) *oauthFlow {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, flow := range a.flows {
		if flow.handoff != "" && constantTimeEqual(flow.handoff, handoff) && time.Since(flow.created) <= flowTTL {
			return flow
		}
	}
	return nil
}

func (a *App) dropHandoff(handoff string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, flow := range a.flows {
		if flow.handoff == handoff {
			delete(a.flows, key)
		}
	}
}

func (a *App) newSession(email string) (id, csrf string, err error) {
	id, csrf = randomToken(), randomToken()
	now := time.Now()
	a.mu.Lock()
	var revoked []string
	for key, session := range a.sessions {
		if now.Sub(session.last) > 24*time.Hour || now.Sub(session.created) > 7*24*time.Hour {
			delete(a.sessions, key)
			revoked = append(revoked, key)
		}
	}
	if len(a.sessions) >= 128 {
		a.mu.Unlock()
		for _, owner := range revoked {
			a.media.RevokeOwner(owner)
		}
		return "", "", errors.New("too many sessions")
	}
	a.sessions[id] = loginSession{csrf: csrf, email: email, created: now, last: now}
	a.mu.Unlock()
	for _, owner := range revoked {
		a.media.RevokeOwner(owner)
	}
	return id, csrf, nil
}

func (a *App) authCapability(r *http.Request) bool {
	if a.cfg.Mode != "desktop" {
		return false
	}
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && constantTimeEqual(value, a.token)
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// googleStart begins the authorization-code flow. Web clients navigate here and
// are redirected to Google. The desktop shell calls it with its bootstrap
// capability, receives a URL to open in the system browser, and later claims the
// session; Google rejects embedded webviews, so the shell never loads consent
// itself.
func (a *App) googleStart(w http.ResponseWriter, r *http.Request) {
	if !a.googleConfigured() {
		failure(w, 503, "AUTH_NOT_CONFIGURED", "Google sign-in is not configured: set MYANIME_GOOGLE_CLIENT_ID, MYANIME_GOOGLE_CLIENT_SECRET and MYANIME_GOOGLE_ALLOWED_EMAILS or MYANIME_GOOGLE_ALLOWED_DOMAINS")
		return
	}
	mode := "web"
	if a.cfg.Mode == "desktop" {
		if !a.authCapability(r) {
			failure(w, 401, "UNAUTHORIZED", "Desktop capability required")
			return
		}
		mode = "desktop"
	}
	clientID, clientSecret, ok := a.clientFor(mode)
	if !ok {
		failure(w, 503, "AUTH_NOT_CONFIGURED", "Google sign-in is not configured for this platform")
		return
	}
	// The redirect must return to the origin the browser is actually using. In
	// development the browser talks to Vite, which proxies to the API with its
	// own Host, so the configured dev origin wins over the request Host.
	base := a.cfg.PublicOrigin
	if base == "" && a.cfg.Development && a.cfg.DevOrigin != "" && mode == "web" {
		base = a.cfg.DevOrigin
	}
	if base == "" {
		scheme := "http"
		if a.secure(r) {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	redirectURI := base + "/api/v1/auth/google/callback"
	state := randomToken()
	verifier := randomToken()
	flow := &oauthFlow{mode: mode, verifier: verifier, redirect: redirectURI, created: time.Now(), returnTo: localPath(r.URL.Query().Get("return"))}
	if mode == "desktop" {
		flow.handoff = randomToken()
		flow.poll = randomToken()
	}
	a.putFlow(state, flow)
	cfg := oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: google.Endpoint, RedirectURL: redirectURI, Scopes: strings.Fields(googleScope)}
	target := cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", pkceChallenge(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
	if mode == "desktop" {
		reply(w, 200, map[string]string{"url": target, "handoff": flow.handoff, "pollToken": flow.poll})
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *App) googleCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	state := query.Get("state")
	flow := a.flow(state)
	if flow == nil {
		a.callbackFailure(w, r, nil, "expired")
		return
	}
	if denied := query.Get("error"); denied != "" {
		a.dropFlow(state)
		a.callbackFailure(w, r, flow, "denied")
		return
	}
	code := query.Get("code")
	if code == "" {
		a.dropFlow(state)
		a.callbackFailure(w, r, flow, "failed")
		return
	}
	clientID, clientSecret, ok := a.clientFor(flow.mode)
	if !ok {
		a.dropFlow(state)
		a.callbackFailure(w, r, flow, "failed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	raw, err := a.exchange(ctx, clientID, clientSecret, flow.redirect, code, flow.verifier)
	if err != nil {
		a.dropFlow(state)
		a.callbackFailure(w, r, flow, "failed")
		return
	}
	identity, err := a.verify(ctx, raw, clientID)
	if err != nil || !identity.EmailVerified || !a.googleAllowed(identity.Email) {
		a.dropFlow(state)
		a.callbackFailure(w, r, flow, "not_allowed")
		return
	}
	id, csrf, err := a.newSession(identity.Email)
	if err != nil {
		a.dropFlow(state)
		a.callbackFailure(w, r, flow, "failed")
		return
	}
	if flow.mode == "desktop" {
		// Keep the flow until the shell claims it; the poll token is the proof.
		a.mu.Lock()
		flow.session = id
		flow.email = identity.Email
		flow.verifier = ""
		a.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(signedInPage))
		return
	}
	// A successful sign-in replaces the browser's previous session so an old
	// cookie cannot keep a second owner alive.
	if c, err := r.Cookie("myanime_session"); err == nil {
		a.mu.Lock()
		_, existed := a.sessions[c.Value]
		delete(a.sessions, c.Value)
		a.mu.Unlock()
		if existed {
			a.media.RevokeOwner(c.Value)
		}
	}
	a.dropFlow(state)
	http.SetCookie(w, &http.Cookie{Name: "myanime_session", Value: id, Path: "/", HttpOnly: true, Secure: a.secure(r), SameSite: http.SameSiteLaxMode, MaxAge: 7 * 86400})
	_ = csrf
	target := flow.returnTo
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	http.Redirect(w, r, target+separator+"auth=ok", http.StatusSeeOther)
}

func (a *App) callbackFailure(w http.ResponseWriter, r *http.Request, flow *oauthFlow, reason string) {
	if flow != nil && flow.mode == "desktop" {
		a.mu.Lock()
		flow.failure = reason
		a.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(failedPage(reason)))
		return
	}
	target := "/"
	if flow != nil {
		target = flow.returnTo
	}
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	http.Redirect(w, r, target+separator+"auth="+url.QueryEscape(reason), http.StatusSeeOther)
}

// desktopClaim hands the finished session to the desktop shell exactly once.
func (a *App) desktopClaim(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Mode != "desktop" {
		failure(w, 404, "NOT_FOUND", "Endpoint not found")
		return
	}
	if !a.authCapability(r) {
		failure(w, 401, "UNAUTHORIZED", "Desktop capability required")
		return
	}
	var body struct {
		Handoff   string `json:"handoff"`
		PollToken string `json:"pollToken"`
	}
	if !decode(w, r, &body) {
		return
	}
	flow := a.flowByHandoff(body.Handoff)
	if flow == nil || flow.mode != "desktop" || !constantTimeEqual(flow.poll, body.PollToken) {
		failure(w, 400, "INVALID_INPUT", "Unknown sign-in attempt")
		return
	}
	if flow.failure != "" {
		a.dropHandoff(body.Handoff)
		failure(w, 403, "DENIED", "Google sign-in was not completed")
		return
	}
	if flow.session == "" {
		reply(w, claimPending, map[string]bool{"pending": true})
		return
	}
	session, email := flow.session, flow.email
	a.dropHandoff(body.Handoff)
	a.mu.Lock()
	csrf := a.sessions[session].csrf
	a.mu.Unlock()
	// The desktop webview runs on mygo://, so it cannot share the web cookie;
	// the shell keeps this session token and sends it as a bearer credential.
	reply(w, claimAccepted, map[string]any{"authenticated": true, "apiToken": session, "csrfToken": csrf, "email": email})
}

const signedInPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Signed in</title>
<style>html{color-scheme:dark}body{margin:0;min-height:100vh;display:grid;place-items:center;background:#090d16;color:#f5f7fc;font:16px/1.5 system-ui,sans-serif}main{max-width:28rem;padding:2rem;text-align:center}h1{font-size:1.5rem;margin:0 0 .5rem}p{margin:0;color:#b7c5dc}</style>
</head><body><main><h1>Signed in</h1><p>Return to the OhMyAnime window to continue.</p></main></body></html>`

func failedPage(reason string) string {
	message := "Google sign-in could not be completed."
	if reason == "not_allowed" {
		message = "That Google account is not allowed on this instance."
	} else if reason == "denied" {
		message = "Sign-in was cancelled."
	}
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Sign-in failed</title>
<style>html{color-scheme:dark}body{margin:0;min-height:100vh;display:grid;place-items:center;background:#090d16;color:#f5f7fc;font:16px/1.5 system-ui,sans-serif}main{max-width:28rem;padding:2rem;text-align:center}h1{font-size:1.5rem;margin:0 0 .5rem}p{margin:0;color:#b7c5dc}</style>
</head><body><main><h1>Sign-in failed</h1><p>` + html.EscapeString(message) + `</p></main></body></html>`
}

func (a *App) sessionInfo(w http.ResponseWriter, r *http.Request) {
	p, authenticated := a.identity(r)
	payload := map[string]any{
		"authenticated": authenticated,
		"desktop":       a.cfg.Mode == "desktop",
		"google":        map[string]any{"configured": a.googleConfigured()},
	}
	if authenticated {
		payload["csrfToken"] = p.csrf
		payload["email"] = p.email
	} else {
		payload["csrfToken"] = ""
	}
	if a.cfg.Mode == "desktop" {
		payload["capability"] = a.authCapability(r)
	}
	reply(w, 200, payload)
}
