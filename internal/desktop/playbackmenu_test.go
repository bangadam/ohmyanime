package desktop

import (
	"bytes"
	"image/png"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"
)

// The Wails/mygo-native surface (App.SetMenu, NewTray, window OnClose)
// cannot be exercised without a native event loop; those paths in Run are
// verified by build only (see issue #12 acceptance note). These tests cover
// the deterministic menu-model logic they drive: labels, enabled state,
// title clamping, quit cleanup, and Set's state machine.

func TestMenuTitle(t *testing.T) {
	tests := []struct {
		name  string
		state PlaybackState
		want  string
	}{
		{"no media", PlaybackState{}, "Nothing playing"},
		{"media without title", PlaybackState{HasMedia: true}, "Untitled"},
		{"blank title", PlaybackState{HasMedia: true, Title: "   "}, "Untitled"},
		{"short title", PlaybackState{HasMedia: true, Title: "Frieren 28"}, "Frieren 28"},
		{"long title clamped", PlaybackState{HasMedia: true, Title: longTitle}, longTitle[:63] + "…"},
		{"multibyte clamped by runes", PlaybackState{HasMedia: true, Title: strings.Repeat("あ", 200)}, string([]rune(strings.Repeat("あ", 200))[:63]) + "…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := menuTitle(tt.state); got != tt.want {
				t.Fatalf("menuTitle(%+v) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

const longTitle = "One Piece Episode 1085: The Legendary Hero's Grand Finale and More Words"

func TestPlayPauseLabel(t *testing.T) {
	if got := playPauseLabel(PlaybackState{HasMedia: true, Playing: true}); got != "Pause" {
		t.Fatalf("playing label = %q, want Pause", got)
	}
	if got := playPauseLabel(PlaybackState{HasMedia: true}); got != "Play" {
		t.Fatalf("paused label = %q, want Play", got)
	}
	// The label names the click's action even with no media, though the
	// item is disabled then.
	if got := playPauseLabel(PlaybackState{}); got != "Play" {
		t.Fatalf("empty label = %q, want Play", got)
	}
}

func TestPlayPauseEnabled(t *testing.T) {
	for _, tt := range []struct {
		state PlaybackState
		want  bool
	}{
		{PlaybackState{}, false},
		{PlaybackState{Playing: true}, false}, // playing without media is inconsistent; still disabled
		{PlaybackState{HasMedia: true}, true},
		{PlaybackState{HasMedia: true, Playing: true}, true},
	} {
		if got := playPauseEnabled(tt.state); got != tt.want {
			t.Fatalf("playPauseEnabled(%+v) = %v, want %v", tt.state, got, tt.want)
		}
	}
}

func TestNewMenuItemsShapeAndQuit(t *testing.T) {
	c := &PlaybackControls{}
	for _, withQuit := range []bool{false, true} {
		items := c.newMenuItems(withQuit)
		var show, playPause, title, quit int
		for _, item := range items {
			switch item.ID {
			case "playback-show":
				show++
			case "playback-playpause":
				playPause++
				if item.IsEnabled() {
					t.Fatal("play/pause enabled before any media")
				}
			case "playback-title":
				title++
				if item.IsEnabled() {
					t.Fatal("title item should be a disabled display row")
				}
				if item.Label != "Nothing playing" {
					t.Fatalf("initial title label = %q", item.Label)
				}
			case "playback-quit":
				quit++
			}
		}
		if show != 1 || playPause != 1 || title != 1 {
			t.Fatalf("withQuit=%v: show=%d playPause=%d title=%d", withQuit, show, playPause, title)
		}
		if withQuit && quit != 1 {
			t.Fatalf("quit item missing from tray menu shape")
		}
		if !withQuit && quit != 0 {
			t.Fatalf("app-menu variant must not duplicate Quit (roles provide it)")
		}
	}
}

func TestSetStateMachine(t *testing.T) {
	c := &PlaybackControls{}
	items := c.newMenuItems(false)
	var playPause, title *mygo.MenuItem
	for _, item := range items {
		switch item.ID {
		case "playback-playpause":
			playPause = item
		case "playback-title":
			title = item
		}
	}

	// No media: disabled, Play.
	_ = c.Set(PlaybackState{})
	if playPause.IsEnabled() {
		t.Fatal("play/pause enabled with no media")
	}
	if got := playPause.Label; got != "Play" {
		t.Fatalf("label = %q, want Play", got)
	}

	// Media paused: enabled, Play.
	_ = c.Set(PlaybackState{HasMedia: true, Title: "Bocchi 12"})
	if !playPause.IsEnabled() {
		t.Fatal("play/pause disabled with media")
	}
	if got := title.Label; got != "Bocchi 12" {
		t.Fatalf("title label = %q", got)
	}
	if got := c.State(); got.Title != "Bocchi 12" || !got.HasMedia || got.Playing {
		t.Fatalf("State() = %+v", got)
	}

	// Playing: enabled, Pause.
	_ = c.Set(PlaybackState{HasMedia: true, Playing: true, Title: "Bocchi 12"})
	if !playPause.IsEnabled() {
		t.Fatal("play/pause disabled while playing")
	}
	if got := playPause.Label; got != "Pause" {
		t.Fatalf("label = %q, want Pause", got)
	}

	// Media removed: back to disabled/Play and the placeholder title.
	_ = c.Set(PlaybackState{})
	if playPause.IsEnabled() {
		t.Fatal("play/pause still enabled after media ended")
	}
	if got := playPause.Label; got != "Play" {
		t.Fatalf("label = %q, want Play", got)
	}
	if got := title.Label; got != "Nothing playing" {
		t.Fatalf("title label = %q, want Nothing playing", got)
	}
}

func TestSetAfterCloseIsNoop(t *testing.T) {
	c := &PlaybackControls{}
	items := c.newMenuItems(true)
	var playPause *mygo.MenuItem
	for _, item := range items {
		if item.ID == "playback-playpause" {
			playPause = item
		}
	}
	// Close runs on quit; the menu items may outlive it, so Set must not
	// touch native state (and must not panic) afterwards.
	c.close()
	_ = c.Set(PlaybackState{HasMedia: true, Playing: true, Title: "x"})
	if playPause.IsEnabled() {
		t.Fatal("native menu item mutated after close")
	}
	if got := c.State(); got != (PlaybackState{}) {
		t.Fatalf("state not reset by close: %+v", got)
	}
	// attachWindow after close must not resurrect native handles.
	c.attachWindow(nil)
}

func TestBeginQuitFlag(t *testing.T) {
	c := &PlaybackControls{}
	if c.isQuitting() {
		t.Fatal("quitting before beginQuit")
	}
	c.beginQuit()
	if !c.isQuitting() {
		t.Fatal("beginQuit did not arm the flag")
	}
	// Flag survives close (quit finishes regardless of cleanup order).
	c.close()
	if !c.isQuitting() {
		t.Fatal("quit flag lost during cleanup")
	}
}

func TestSetConcurrent(t *testing.T) {
	c := &PlaybackControls{}
	_ = c.newMenuItems(false)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = c.Set(PlaybackState{HasMedia: i%2 == 0, Playing: i%3 == 0, Title: "t"})
			_ = c.State()
		}(i)
	}
	wg.Wait()
}

func TestTrayIconPNG(t *testing.T) {
	pngBytes := trayIconPNG()
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("tray icon is not a PNG: %v", err)
	}
	if got := img.Bounds().Dx(); got != 32 {
		t.Fatalf("icon width = %d, want 32", got)
	}
	// Template icons must be black-on-transparent; check the apex pixel.
	r, g, b, a := img.At(16, 16).RGBA()
	if r != 0 || g != 0 || b != 0 || a == 0 {
		t.Fatalf("apex pixel = rgba(%d,%d,%d,%d), want opaque black", r, g, b, a)
	}
	// A corner must be transparent.
	_, _, _, a = img.At(0, 0).RGBA()
	if a != 0 {
		t.Fatal("corner is not transparent; template tinting would fail")
	}
}
