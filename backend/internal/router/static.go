package router

import (
	"io/fs"
	"net/http"
	"strings"
)

// httpFS adapts an fs.FS for gin's StaticFS, hiding directory listings and everything at or below
// a dot-named file or directory (.git, .env, ...).
func httpFS(fsys fs.FS) http.FileSystem {
	return noListFS{http.FS(fsys)}
}

type noListFS struct{ http.FileSystem }

func (n noListFS) Open(name string) (http.File, error) {
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") {
			return nil, fs.ErrNotExist
		}
	}
	f, err := n.FileSystem.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, fs.ErrNotExist
	}
	return f, nil
}
