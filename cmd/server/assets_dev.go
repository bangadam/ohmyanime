//go:build !release

package main

import "io/fs"

func frontendAssets() (fs.FS, error) { return nil, nil }
