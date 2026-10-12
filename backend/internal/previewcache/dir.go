package previewcache

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Path is the file name name directly inside it. It returns "" when name (built from a row ID, which
// an imported bundle or a URL can make hostile) would put the file anywhere else.
func (c *Cache) Path(name string) string {
	if c.dir == "" {
		return ""
	}
	p := filepath.Join(c.dir, name)
	if filepath.Dir(p) != c.dir {
		return ""
	}
	return p
}

// Size is the total size of the finished previews; a directory that is not there is empty.
func (c *Cache) Size() (int64, error) {
	files, err := list(c.dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, f := range files {
		total += f.size
	}
	return total, nil
}

// prune deletes the oldest finished previews (by modification time) until the rest is at most
// c.maxBytes. The file keep (the one just made) is never deleted. A file that cannot be deleted is
// skipped and the first such error is returned after the pass.
func (c *Cache) prune(keep string) error {
	files, err := list(c.dir)
	if err != nil {
		return err
	}
	var total int64
	for _, f := range files {
		total += f.size
	}
	slices.SortFunc(files, func(a, b file) int { return a.modTime.Compare(b.modTime) })
	var first error
	for _, f := range files {
		if total <= c.maxBytes {
			break
		}
		if f.path == keep {
			continue
		}
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			if first == nil {
				first = err
			}
			continue
		}
		total -= f.size
	}
	return first
}

type file struct {
	path    string
	size    int64
	modTime time.Time
}

// list returns the finished preview files directly inside dir (a missing dir has none). A name
// that starts with a dot is a conversion in progress (media makes ".<name>.ffmpeg.tmp" and renames
// it when done): it is not counted and never deleted from under the ffmpeg writing it.
func list(dir string) ([]file, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := make([]file, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() { // gone since ReadDir, or not a preview file
			continue
		}
		files = append(files, file{path: filepath.Join(dir, e.Name()), size: info.Size(), modTime: info.ModTime()})
	}
	return files, nil
}
