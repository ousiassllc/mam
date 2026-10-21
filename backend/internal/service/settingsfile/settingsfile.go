// Package settingsfile reads and writes the JSON settings files of the data directory that the
// settings screen edits (llm.json, models.json). The file may hold keys the screen does not manage
// (a hand-written override of a base URL, a retry policy, ...), so the screen decodes it as a generic
// object, changes only its own keys and writes the object back: what it does not know is preserved.
package settingsfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// Object is the top-level JSON object of a settings file, one raw value per key.
type Object map[string]json.RawMessage

// Read returns the object stored at path. A file that does not exist is an empty object: the
// settings are then the defaults.
func Read(path string) (Object, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the path is the settings file of the data directory
	if errors.Is(err, fs.ErrNotExist) {
		return Object{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read settings file: %w", err)
	}
	var obj Object
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("decode settings file %s: %w", path, err)
	}
	if obj == nil { // the literal null
		obj = Object{}
	}
	return obj, nil
}

// Marshal is the bytes Write stores for obj: indented, so that a person can read and edit it.
func Marshal(obj Object) ([]byte, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode settings file: %w", err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return nil, fmt.Errorf("encode settings file: %w", err)
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// Write stores obj at path atomically (a temporary file in the same directory, renamed over path), and
// creates the directory when it does not exist. An empty object removes the file: no file and an empty
// one both select the defaults, and a missing file is what a fresh installation has.
func Write(path string, obj Object) error {
	if len(obj) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove settings file: %w", err)
		}
		return nil
	}
	data, err := Marshal(obj)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}
