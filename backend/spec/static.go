package spec

import (
	"embed"
	"io/fs"
)

//go:embed openapi.json
var embeddedFS embed.FS

// FS returns the embedded spec filesystem containing openapi.json.
func FS() fs.FS {
	return embeddedFS
}
