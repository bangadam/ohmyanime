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
	application, err := app.New(app.Config{
		DataDir: dataDir, FrontendFS: frontendFS, Mode: "desktop",
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
