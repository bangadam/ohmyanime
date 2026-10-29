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
	"runtime"
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
	// One running instance per data directory: a second launch signals the
	// first with its arguments and exits before the database is opened, so
	// there is never a second writer. The lock is tied to the data
	// directory, not the executable, keeping development and packaged data
	// apart.
	lock, primary, err := TakeInstanceLock(instanceLockPath(dataDir), os.Args[1:], nil)
	if err != nil {
		return err
	}
	if !primary {
		return nil
	}
	defer func() { result = errors.Join(result, lock.Close()) }()
	// Title deep links. The scheme is declared for packaging in mygo.json
	// (macOS Info.plist); this runtime registration makes the executable
	// the user-level handler on Windows and Linux.
	if err := mygo.App.RegisterURLScheme(titleLinkScheme); err != nil {
		return fmt.Errorf("register %s URL scheme: %w", titleLinkScheme, err)
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
	// PlaybackControls backs the app menu and tray; the frontend pushes
	// state with call("PlaybackControls.Set", ...) and subscribes to
	// "playback:toggle". Closed on quit so menus stop mutating native
	// state.
	controls := &PlaybackControls{}
	mygo.Bind(controls)
	defer controls.close()
	mygo.Bind(bootstrap)
	// Updater backs the manual, consent-driven update flow in Preferences;
	// it never checks or installs on its own.
	mygo.Bind(&Updater{})
	// Title deep links reach the page as one typed event; the URL itself
	// never enters the WebView. Cold launches (the URL the app was started
	// with) and warm launches (OnOpenURL, or a signal from a second
	// instance) all pass through ParseTitleLink, so anything outside the
	// grammar is dropped before navigation.
	titleLinks := &titleLinkDelivery{}
	mygo.App.OnOpenURL(func(raw string) {
		if link, ok := ParseTitleLink(raw); ok {
			titleLinks.deliver(link)
		}
	})
	lock.setLaunchHandler(func(args []string) {
		if link, ok := firstTitleLink(args); ok {
			titleLinks.deliver(link)
		}
	})
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
		controls.attachWindow(window)
		titleLinks.attach(window)
		window.Page().OnWillNavigate(func(event *mygo.NavigateEvent) {
			u, err := url.Parse(event.URL)
			if err != nil || !allowedPage(u, origin) {
				event.PreventDefault()
			}
		})
		window.Page().SetPermissionHandler(func(mygo.PermissionRequest) bool { return false })
		// App menu: platform roles carry Quit (RoleAppMenu on darwin,
		// RoleFileMenu elsewhere) plus a Playback submenu shared with the
		// tray. Unsupported platform actions simply do not exist here:
		// the tray is darwin-only, roles are platform-built by mygo.
		appMenu := mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu},
			{Label: "Playback", Submenu: controls.newMenuItems(false)},
		})
		mygo.App.SetMenu(appMenu)
		// Close-vs-quit policy (documented in playbackmenu.go): on darwin
		// the window close button hides the window so the app stays
		// reachable from the menu bar, tray and Dock; quitting is a
		// separate action that always terminates and runs Run's cleanup,
		// which closes the listener, the HTTP server and playback. On
		// other platforms closing the last window quits (mygo default).
		if runtime.GOOS == "darwin" {
			window.OnClose(func(event *mygo.CloseEvent) {
				if !controls.isQuitting() {
					event.PreventDefault()
					window.Hide()
				}
			})
			if tray, err := mygo.NewTray(mygo.TrayOptions{
				ID: "playback", Icon: trayIconPNG(), IconIsTemplate: true,
				ToolTip: "MyAnime", Menu: mygo.NewMenu(controls.newMenuItems(true)),
			}); err == nil {
				_ = tray
			}
		}
		// Runs before mygo closes windows on quit, arming the close
		// policy above to stand aside for a real quit.
		mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { controls.beginQuit() })
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
