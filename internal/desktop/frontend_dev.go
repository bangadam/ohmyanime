//go:build !release

package desktop

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

func frontend() (pageURL, origin string, assets fs.FS, err error) {
	if raw := os.Getenv("MYANIME_DEV_URL"); raw != "" {
		u, parseErr := url.Parse(raw)
		if parseErr != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", "", nil, errors.New("MYANIME_DEV_URL must be a loopback HTTP(S) URL without credentials, query or fragment")
		}
		ip := net.ParseIP(u.Hostname())
		if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", "", nil, errors.New("MYANIME_DEV_URL must use localhost or a loopback IP address")
		}
		return u.String(), u.Scheme + "://" + u.Host, nil, nil
	}
	dir := os.Getenv("MYANIME_FRONTEND_DIR")
	if dir == "" {
		dir = filepath.Join("web", "dist")
	}
	info, statErr := os.Stat(filepath.Join(dir, "index.html"))
	if statErr != nil || !info.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("frontend assets missing in %q: build web/dist with explicit production-build approval, set MYANIME_FRONTEND_DIR to existing built assets, or start the web development server and set MYANIME_DEV_URL", dir)
	}
	assets = os.DirFS(dir)
	mygo.SetFrontend(assets)
	return "/", "", assets, nil
}
