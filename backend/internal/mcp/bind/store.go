package bind

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// FileName is the settings file in the data directory.
const FileName = "mcp.json"

const keyBindAddress = "bind_address"

// ErrMalformed matches (errors.Is) a settings file whose content cannot be read: the JSON is invalid or is not an object, or bind_address is not a string.
// The person can fix it by editing the file.
var ErrMalformed = errors.New("mcp.json is malformed")

// Load returns the saved bind address, or Default when there is no file or no such key. A file that
// cannot be read is an error. The address is not validated here: the interface may not exist yet when
// the app starts (Tailscale starts later), which the caller handles.
func Load(path string) (string, error) {
	obj, err := settingsfile.Read(path)
	if err != nil {
		var syntax *json.SyntaxError
		var typed *json.UnmarshalTypeError
		if errors.As(err, &syntax) || errors.As(err, &typed) {
			return "", fmt.Errorf("%w: %w", ErrMalformed, err)
		}
		return "", err
	}
	raw, ok := obj[keyBindAddress]
	if !ok {
		return Default, nil
	}
	var addr string
	if err := json.Unmarshal(raw, &addr); err != nil || strings.TrimSpace(addr) == "" {
		return "", fmt.Errorf("%s: %q must be a string: %w", path, keyBindAddress, ErrMalformed)
	}
	return strings.TrimSpace(addr), nil
}

// Save stores addr, keeping any other key of the file. The Default address removes the key (and the
// file when nothing else is in it), so that a fresh installation and a reset look the same.
func Save(path, addr string) error {
	obj, err := settingsfile.Read(path)
	if err != nil {
		return err
	}
	if addr == Default {
		delete(obj, keyBindAddress)
	} else {
		raw, err := json.Marshal(addr)
		if err != nil {
			return err
		}
		obj[keyBindAddress] = raw
	}
	return settingsfile.Write(path, obj)
}
