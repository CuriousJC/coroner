//go:build webui

// Package webui holds the built front end from web/, compiled into the binary.
//
// Behind the webui build tag because the assets only exist after `npm run
// build`: `make all` builds them and sets the tag, while a plain `go build` or
// `go test` compiles none.go instead and needs no node toolchain.
//
// The bundle holds code only. The writing it shows is fetched at runtime from
// `coroner serve`, so nothing private is ever compiled in.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the built front end.
func Assets() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	return sub, true
}
