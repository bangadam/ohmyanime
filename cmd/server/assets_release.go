//go:build release

package main

import (
	"io/fs"
	assets "myanime"
)

func frontendAssets() (fs.FS, error) { return assets.Frontend() }
