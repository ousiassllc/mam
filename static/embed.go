// Package static embeds the built front-end assets (esbuild + Tailwind output)
// so the server ships as a single binary. src/dist/ is produced by `make assets` (or `make build`);
// only src/dist/.gitkeep is committed, so `go build` works before the assets are built.
package static

import (
	"embed"
	"io/fs"
)

//go:embed all:src/dist
var embedded embed.FS

// Dist returns the built assets rooted at src/dist/ (app.js, app.css, ...).
func Dist() fs.FS {
	sub, err := fs.Sub(embedded, "src/dist")
	if err != nil {
		panic(err) // src/dist is a compile-time embedded directory; Sub cannot fail
	}
	return sub
}
