package catalog

import (
	"errors"
	"io/fs"
)

// Overlay returns the content of the embedded file with the name.
func (c *Catalog) Overlay(name string) ([]byte, bool) {
	b, ok := c.Overlays[name]
	return b, ok
}

// loadOverlays reads the files of data/overlays (the directory is optional).
func loadOverlays(fsys fs.FS) (map[string][]byte, error) {
	entries, err := fs.ReadDir(fsys, "data/overlays")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if files[e.Name()], err = fs.ReadFile(fsys, "data/overlays/"+e.Name()); err != nil {
			return nil, err
		}
	}
	return files, nil
}
