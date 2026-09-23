//go:build !webui

package webui

import "io/fs"

// Assets reports that this binary was built without the front end. `coroner
// serve` falls back to the static timeline page.
func Assets() (fs.FS, bool) { return nil, false }
