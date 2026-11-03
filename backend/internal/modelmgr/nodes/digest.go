package nodes

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// TreeDigest is the SHA-256 of the files below dir: for every regular file in the order of the paths, its slash path and its SHA-256. Every
// file counts, a __pycache__ too: the engine runs with PYTHONDONTWRITEBYTECODE=1, so nothing of Python's own is expected there, and a .pyc put
// there would run in place of the source it claims to be of. A symbolic link or any other file that is not regular is an error: an
// extraction makes none, so one is something put there afterwards.
func TreeDigest(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			return fmt.Errorf("nodes: %s is not a regular file", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sum, err := fileSum(path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%s\n", filepath.ToSlash(rel), sum)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fileSum(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- a file below the folder of a node that mam installed
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
