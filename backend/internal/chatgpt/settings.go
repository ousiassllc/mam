package chatgpt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// SettingsFile is the file of the data directory that holds the on/off switch of the image generation. It holds no secret.
const SettingsFile = "chatgpt.json"

type settingsData struct {
	ImageGeneration struct {
		Enabled bool `json:"enabled"`
		// ConfirmedAt is when the user confirmed the risks (kept for the record; a switch that was turned off asks again).
		ConfirmedAt time.Time `json:"confirmed_at,omitzero"`
	} `json:"image_generation"`
}

// settings is the switch of the unofficial image generation. The file is read at every call, so a change applies at
// once, also to a call already queued; a file that is missing or unreadable means OFF.
type settings struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

func (s *settings) read() (settingsData, error) {
	var d settingsData
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("chatgpt: read %s: %w", SettingsFile, err)
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return settingsData{}, nil // damaged by hand: OFF is the safe reading, the next save replaces it
	}
	return d, nil
}

func (s *settings) imageEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.read()
	return err == nil && d.ImageGeneration.Enabled
}

func (s *settings) setImage(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.read()
	if err != nil {
		return err
	}
	d.ImageGeneration.Enabled = enabled
	if enabled {
		d.ImageGeneration.ConfirmedAt = s.now()
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.path, append(raw, '\n'), 0o600)
}
