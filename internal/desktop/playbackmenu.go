// Playback menu and tray controls for issue #12 (roadmap 3.1).
//
// Frontend contract (web side pushes state, Go side requests actions):
//
//	call("PlaybackControls.Set", { hasMedia, playing, title })
//	on("playback:toggle", (req) => { /* req.action === "toggle" */ })
//
// Close-vs-quit policy, enforced in Run():
//
//   - Quit (App.Quit, the app menu Quit role, the tray Quit item, or the
//     last window closing on non-darwin) always terminates the process:
//     App.OnBeforeQuit marks the app as quitting so window OnClose cannot
//     cancel it, and Run's deferred cleanup closes the loopback listener,
//     the HTTP server and the application (which stops playback by taking
//     the page down).
//   - Closing the window on darwin hides the window instead of quitting:
//     the app stays available from its Dock icon, the menu bar and the
//     tray. This matches macOS convention. On Windows and Linux closing
//     the last window quits (mygo default), because there is no menu bar
//     or Dock to keep the app reachable from.
//   - Unsupported actions are hidden, not merely disabled: the tray exists
//     only on darwin, and darwin-only roles (About, Hide, ...) come from
//     the RoleAppMenu mygo builds for that platform.
package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/egoist/mygo"
)

// PlaybackState is the player snapshot the frontend pushes over native IPC.
type PlaybackState struct {
	HasMedia bool   `json:"hasMedia"`
	Playing  bool   `json:"playing"`
	Title    string `json:"title"`
}

// toggleRequest is the payload of the "playback:toggle" event the menus send
// to the frontend page when the user picks Play/Pause.
type toggleRequest struct {
	Action string `json:"action"`
}

// playbackToggle is declared at package level so mygo registers it once; the
// frontend subscribes by event name.
var playbackToggle = mygo.NewEvent[toggleRequest]("playback:toggle")

// menuTitleLimit caps the title shown in menus so a long episode title does
// not stretch the tray menu across the screen.
const menuTitleLimit = 64

// menuTitle returns the clamped title, or a placeholder when nothing plays.
func menuTitle(state PlaybackState) string {
	if !state.HasMedia {
		return "Nothing playing"
	}
	title := strings.TrimSpace(state.Title)
	if title == "" {
		return "Untitled"
	}
	if utf8.RuneCountInString(title) <= menuTitleLimit {
		return title
	}
	runes := []rune(title)
	return string(runes[:menuTitleLimit-1]) + "…"
}

// playPauseLabel names the Play/Pause item for the current state: the label
// always names the action the click will perform.
func playPauseLabel(state PlaybackState) string {
	if state.Playing {
		return "Pause"
	}
	return "Play"
}

// playPauseEnabled reports whether playback control is possible: disabled
// whenever the player has no media loaded.
func playPauseEnabled(state PlaybackState) bool { return state.HasMedia }

// PlaybackControls is the bound service backing the app menu and tray.
// The frontend calls Set; the menus call back through the playbackToggle
// event and mygo.App.Quit.
type PlaybackControls struct {
	mu        sync.Mutex
	state     PlaybackState
	window    *mygo.Window
	playPause []*mygo.MenuItem
	titles    []*mygo.MenuItem
	closed    bool
	quitting  atomic.Bool
}

// Set updates the playback snapshot and every attached menu item. It is the
// single entry point the frontend uses; menu items change through mygo's
// thread-safe setters, so this may run on any goroutine.
func (c *PlaybackControls) Set(state PlaybackState) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.state = state
	playPause := c.playPause
	titles := c.titles
	window := c.window
	c.mu.Unlock()

	label := playPauseLabel(state)
	enabled := playPauseEnabled(state)
	title := menuTitle(state)
	for _, item := range playPause {
		item.SetLabel(label)
		item.SetEnabled(enabled)
	}
	for _, item := range titles {
		item.SetLabel(title)
	}
	if window != nil {
		window.SetTitle(title)
	}
	return nil
}

// State returns the current snapshot (used by tests and diagnostics).
func (c *PlaybackControls) State() PlaybackState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// attachWindow records the main window the Playback menu controls. It runs
// once from App.WhenReady, before any Set call can race it.
func (c *PlaybackControls) attachWindow(window *mygo.Window) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.window = window
}

// close releases every native handle. It runs on the quit path only, after
// which Set is a no-op: quitting stops menus from mutating native state.
func (c *PlaybackControls) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.window = nil
	c.playPause = nil
	c.titles = nil
	c.state = PlaybackState{}
}

// beginQuit marks the app as quitting before mygo closes windows, so the
// darwin close-to-hide policy in Run stands aside for a real quit.
func (c *PlaybackControls) beginQuit() { c.quitting.Store(true) }

// isQuitting reports whether a quit is in progress, which the window
// OnClose policy consults to distinguish user-close from quit.
func (c *PlaybackControls) isQuitting() bool { return c.quitting.Load() }

// toggleAction asks the frontend page to play or pause; it is only reachable
// while the Play/Pause item is enabled, i.e. media exists.
func (c *PlaybackControls) toggleAction() {
	c.mu.Lock()
	window := c.window
	c.mu.Unlock()
	if window == nil || window.IsDestroyed() {
		return
	}
	_ = playbackToggle.Emit(window, toggleRequest{Action: "toggle"})
}

// showWindow reopens and focuses the main window (issue #12: "reopening
// focuses the window"; mygo Show both shows and focuses).
func (c *PlaybackControls) showWindow() {
	c.mu.Lock()
	window := c.window
	c.mu.Unlock()
	if window == nil || window.IsDestroyed() {
		return
	}
	window.Show()
}

// newMenuItems builds the shared Playback menu body. quit controls whether a
// Quit item is appended: the app menu gets Quit from its platform roles
// (RoleAppMenu on darwin, RoleFileMenu elsewhere), while the tray menu has
// no roles and carries its own.
func (c *PlaybackControls) newMenuItems(quit bool) []*mygo.MenuItem {
	playPause := &mygo.MenuItem{
		ID: "playback-playpause", Label: playPauseLabel(PlaybackState{}),
		Disabled: !playPauseEnabled(PlaybackState{}),
		Click:    func(*mygo.MenuItem, *mygo.Window) { c.toggleAction() },
	}
	title := &mygo.MenuItem{
		ID: "playback-title", Label: menuTitle(PlaybackState{}), Disabled: true,
	}
	items := []*mygo.MenuItem{
		{
			ID: "playback-show", Label: "Show MyAnime",
			Click: func(*mygo.MenuItem, *mygo.Window) { c.showWindow() },
		},
		mygo.Separator(),
		title,
		playPause,
	}
	if quit {
		items = append(items, mygo.Separator(), &mygo.MenuItem{
			ID: "playback-quit", Label: "Quit MyAnime",
			Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() },
		})
	}
	c.attachItems(playPause, title)
	return items
}

// attachItems records the state-backed items; separate from attach so the
// app menu and the tray menu can each own a pair that stays in sync.
func (c *PlaybackControls) attachItems(playPause, title *mygo.MenuItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.playPause = append(c.playPause, playPause)
	c.titles = append(c.titles, title)
}

// trayIconPNG renders the 32x32 template icon (a play triangle) the darwin
// tray shows. Generated in code so no binary asset ships with the package.
func trayIconPNG() []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	black := color.RGBA{R: 0, G: 0, B: 0, A: 0xff}
	// Triangle: base x=8 from y=7..25, apex at (24,16).
	for y := range size {
		for x := range size {
			if x >= 8 && x <= 24 {
				spread := (x - 8) * 9 / 16
				if y >= 16-spread && y <= 16+spread {
					img.SetRGBA(x, y, black)
				}
			}
		}
	}
	var buf bytes.Buffer
	// png.Encode to an in-memory buffer cannot fail.
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
