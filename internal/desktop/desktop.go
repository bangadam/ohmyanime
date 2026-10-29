// Package desktop hosts MyAnime in MyGo v0.7.1 (Go 1.27.3 or newer).
package desktop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"myanime/internal/app"
)

// Session is delivered only to the trusted page over native IPC, never in URLs.
type Session struct {
	APIBase   string `json:"apiBase"`
	APIToken  string `json:"apiToken"`
	CSRFToken string `json:"csrfToken"`
	Desktop   bool   `json:"desktop"`
}

// Bootstrap is the only bound application service.
type Bootstrap struct {
	mu      sync.RWMutex
	session Session
	active  bool
}

func (b *Bootstrap) Get() (Session, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.active {
		return Session{}, errors.New("desktop session is unavailable")
	}
	return b.session, nil
}

func (b *Bootstrap) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.active = false
	b.session = Session{}
}

// Auth opens the system browser for Google sign-in. Embedded webviews are not
// allowed to run Google consent, so the shell hands the URL to the OS instead.
type Auth struct{}

func (Auth) Open(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return errors.New("sign-in URL must be a plain HTTPS URL")
	}
	if u.Hostname() != "accounts.google.com" {
		return errors.New("sign-in URL must point at Google")
	}
	return mygo.Shell.OpenExternal(u.String())
}

// Run must be called on the main goroutine, as required by native UI toolkits.
func Run() (result error) {
	mygo.App.SetName("MyAnime")
	pageURL, origin, frontendFS, err := frontend()
	if err != nil {
		return err
	}
	dataDir := os.Getenv("MYANIME_DATA_DIR")
	if dataDir == "" {
		dataDir, err = mygo.App.Path(mygo.PathUserData)
		if err != nil {
			return fmt.Errorf("desktop data directory: %w", err)
		}
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("create desktop data directory: %w", err)
	}
	if _, err := mygo.App.Path(mygo.PathCache); err != nil {
		return fmt.Errorf("desktop cache directory: %w", err)
	}
	google := app.GoogleConfig{
		ClientID:            os.Getenv("MYANIME_GOOGLE_CLIENT_ID"),
		ClientSecret:        os.Getenv("MYANIME_GOOGLE_CLIENT_SECRET"),
		DesktopClientID:     os.Getenv("MYANIME_GOOGLE_DESKTOP_CLIENT_ID"),
		DesktopClientSecret: os.Getenv("MYANIME_GOOGLE_DESKTOP_CLIENT_SECRET"),
		AllowedEmails:       splitList(os.Getenv("MYANIME_GOOGLE_ALLOWED_EMAILS")),
		AllowedDomains:      splitList(os.Getenv("MYANIME_GOOGLE_ALLOWED_DOMAINS")),
	}
	if google.ClientID == "" || google.ClientSecret == "" || (len(google.AllowedEmails) == 0 && len(google.AllowedDomains) == 0) {
		return errors.New("Google sign-in requires MYANIME_GOOGLE_CLIENT_ID, MYANIME_GOOGLE_CLIENT_SECRET and MYANIME_GOOGLE_ALLOWED_EMAILS or MYANIME_GOOGLE_ALLOWED_DOMAINS")
	}
	application, err := app.New(app.Config{
		DataDir: dataDir, FrontendFS: frontendFS, Mode: "desktop", Google: google,
		Development: origin != "", DevOrigin: origin,
	})
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, application.Close()) }()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("desktop loopback listener: %w", err)
	}
	defer listener.Close()
	credentials := application.Bootstrap()
	bootstrap := &Bootstrap{active: true, session: Session{
		APIBase:  "http://" + listener.Addr().String(),
		APIToken: credentials["apiToken"], CSRFToken: credentials["csrfToken"], Desktop: true,
	}}
	if bootstrap.session.APIToken == "" {
		return errors.New("desktop application did not issue an API capability")
	}
	mygo.Bind(bootstrap)
	mygo.Bind(Auth{})
	server := &http.Server{
		Handler: exactHost(listener.Addr().String(), application.Handler()), ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErrors <- err
		if err != nil {
			mygo.App.Quit()
		}
	}()
	defer func() {
		bootstrap.close()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		shutdownErr := server.Shutdown(ctx)
		if shutdownErr != nil {
			_ = server.Close()
		}
		result = errors.Join(result, shutdownErr, <-serveErrors)
	}()
	mygo.App.WhenReady(func() {
		options := mygo.WindowOptions{
			Title: "MyAnime", URL: pageURL, Width: 1280, Height: 820,
			MinWidth: 720, MinHeight: 480, StateKey: "main",
			BackgroundColor: "#10121a",
		}
		if origin != "" {
			options.Page = mygo.PageOptions{TrustedOrigins: []string{origin}}
		}
		window := mygo.NewWindow(options)
		window.Page().OnWillNavigate(func(event *mygo.NavigateEvent) {
			u, err := url.Parse(event.URL)
			if err != nil || !allowedPage(u, origin) {
				event.PreventDefault()
			}
		})
		window.Page().SetPermissionHandler(func(mygo.PermissionRequest) bool { return false })
	})
	return mygo.App.Run()
}

func allowedPage(u *url.URL, devOrigin string) bool {
	if u.User != nil {
		return false
	}
	origin := u.Scheme + "://" + u.Host
	if devOrigin != "" {
		return origin == devOrigin
	}
	return origin == "mygo://localhost" || origin == "http://mygo.localhost"
}

// Reject forged Host values before application auth, including DNS rebinding.
func exactHost(host string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func splitList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
