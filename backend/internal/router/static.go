package router

import (
	"io/fs"
	"net/http"
)

// httpFS adapts an fs.FS for gin's StaticFS, hiding directory listings and dotfiles.
func httpFS(fsys fs.FS) http.FileSystem {
	return noListFS{http.FS(fsys)}
}

type noListFS struct{ http.FileSystem }

func (n noListFS) Open(name string) (http.File, error) {
	f, err := n.FileSystem.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() || (len(info.Name()) > 0 && info.Name()[0] == '.') {
		_ = f.Close()
		return nil, fs.ErrNotExist
	}
	return f, nil
}
