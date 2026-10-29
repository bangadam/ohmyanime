//go:build release

package desktop

import (
	"fmt"
	"io/fs"

	"github.com/egoist/mygo"

	assets "myanime"
)

// Release binaries load only their embedded frontend, never environment URLs.
func frontend() (pageURL, origin string, frontendFS fs.FS, err error) {
	frontendFS, err = assets.Frontend()
	if err != nil {
		return "", "", nil, fmt.Errorf("embedded frontend: %w", err)
	}
	info, err := fs.Stat(frontendFS, "index.html")
	if err != nil {
		return "", "", nil, fmt.Errorf("embedded frontend index: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("embedded frontend index.html is not a regular file")
	}
	mygo.SetFrontend(frontendFS)
	return "/", "", frontendFS, nil
}
