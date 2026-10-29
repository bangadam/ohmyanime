//go:build release

// Package assets supplies the frontend embedded in release binaries.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:web/dist
var bundled embed.FS

func Frontend() (fs.FS, error) { return fs.Sub(bundled, "web/dist") }
