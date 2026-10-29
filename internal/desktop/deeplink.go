package desktop

import (
	"net/url"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// titleLinkScheme is the URL scheme of title deep links. Registering the
// scheme with the operating system is a packaging concern: it is declared
// as urlSchemes in mygo.json (Info.plist on macOS, build metadata
// elsewhere) and taken at runtime with mygo.App.RegisterURLScheme on the
// other platforms. This file implements the handler side only.
const titleLinkScheme = "myanime"

// Title kinds of the allowlisted deep-link grammar.
const (
	titleKindAnime = "anime"
	titleKindManga = "manga"
)

// maxTitleIDLen bounds the title IDs accepted from URLs.
const maxTitleIDLen = 128

// TitleLink is a validated deep-link target: one anime or manga title ID,
// nothing else. It is delivered to the trusted page over native IPC, never
// in a URL.
type TitleLink struct {
	Kind    string `json:"kind"`
	TitleID string `json:"titleId"`
}

// titleLinkEvent navigates the page to a title. Declared at package level
// so a generated TypeScript client would include it; the web app subscribes
// with event<TitleLink>("deeplink:title").
var titleLinkEvent = mygo.NewEvent[TitleLink]("deeplink:title")

// ParseTitleLink parses the allowlisted deep-link grammar:
//
//	myanime://anime/<titleID>
//	myanime://manga/<titleID>
//
// The scheme must be myanime, the host exactly "anime" or "manga", and the
// path a single non-empty segment of letters, digits, ':', '-' or '_'
// within maxTitleIDLen. Userinfo, ports, queries and fragments are
// rejected, and so is every other URL: external schemes never reach the
// page through this parser. ok reports whether raw is a valid title link.
func ParseTitleLink(raw string) (link TitleLink, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return TitleLink{}, false
	}
	if u.Scheme != titleLinkScheme || u.Opaque != "" || u.User != nil {
		return TitleLink{}, false
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.RawFragment != "" {
		return TitleLink{}, false
	}
	kind := strings.ToLower(u.Host)
	if kind != titleKindAnime && kind != titleKindManga {
		return TitleLink{}, false
	}
	if u.Path == "" || u.Path[0] != '/' || u.Path[len(u.Path)-1] == '/' {
		return TitleLink{}, false
	}
	// Exactly one segment; a percent-encoded separator decodes to '/' and
	// is rejected here too.
	titleID := u.Path[1:]
	if strings.Contains(titleID, "/") || !validTitleID(titleID) {
		return TitleLink{}, false
	}
	return TitleLink{Kind: kind, TitleID: titleID}, true
}

// validTitleID reports whether titleID is a safe single title identifier.
func validTitleID(titleID string) bool {
	if titleID == "" || len(titleID) > maxTitleIDLen {
		return false
	}
	for _, c := range titleID {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == ':' || c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// firstTitleLink returns the first valid title link among launch arguments,
// so a second instance can hand its deep link to the running one.
func firstTitleLink(args []string) (TitleLink, bool) {
	for _, arg := range args {
		if link, ok := ParseTitleLink(arg); ok {
			return link, true
		}
	}
	return TitleLink{}, false
}

// titleLinkDelivery routes validated title links to the main window, from
// any goroutine. Links that arrive before the window exists (a cold launch
// can open its URL before the UI is built) are held and flushed when the
// window attaches; mygo itself holds events until the page's DOM is ready.
type titleLinkDelivery struct {
	mu      sync.Mutex
	window  *mygo.Window
	pending []TitleLink
}

// attach registers the main window and flushes any pending links to it.
func (d *titleLinkDelivery) attach(window *mygo.Window) {
	d.mu.Lock()
	d.window = window
	links := d.pending
	d.pending = nil
	d.mu.Unlock()
	for _, link := range links {
		emitTitleLink(window, link)
	}
}

// deliver sends link to the window, or holds it until one exists.
func (d *titleLinkDelivery) deliver(link TitleLink) {
	d.mu.Lock()
	window := d.window
	if window == nil {
		d.pending = append(d.pending, link)
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	emitTitleLink(window, link)
}

// emitTitleLink navigates the page and raises the window. Emit and Focus
// are safe from any goroutine.
func emitTitleLink(window *mygo.Window, link TitleLink) {
	_ = titleLinkEvent.Emit(window, link)
	window.Focus()
}
