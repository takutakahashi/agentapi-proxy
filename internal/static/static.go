package static

import (
	"embed"
	"io/fs"
)

//go:embed public
var embeddedFS embed.FS

// PublicFS returns the embedded public filesystem.
// Files are served under the "public" directory of this package.
func PublicFS() fs.FS {
	sub, err := fs.Sub(embeddedFS, "public")
	if err != nil {
		panic("static: failed to create sub-filesystem: " + err.Error())
	}
	return sub
}
