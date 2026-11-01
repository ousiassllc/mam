package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
)

// LocalAIDirName is the directory next to the executable that holds the local runtimes (GPT-SoVITS, MuseTalk), one
// directory per runtime id (spec/architecture/model-manager/local-runtime.md §2). Like the comfyui directory it is the only place a runtime
// persists in, so that the uninstaller (which removes the install directory) leaves nothing of it behind.
const LocalAIDirName = "localai"

// LocalAIRoot is the localai directory next to the executable at exe.
func LocalAIRoot(exe string) string { return filepath.Join(filepath.Dir(exe), LocalAIDirName) }

// RuntimeLayout is the places of one runtime below Root (<exe dir>/localai/<runtime id>).
type RuntimeLayout struct{ Root string }

// ForRuntime is the RuntimeLayout of the runtime id below the localai directory root.
func ForRuntime(root, id string) RuntimeLayout { return RuntimeLayout{Root: filepath.Join(root, id)} }

// EnvDir is the extracted package: the Python environment and the vendor's program. It is replaced as a whole when the
// runtime is updated; nothing of the user lives here.
func (l RuntimeLayout) EnvDir() string { return filepath.Join(l.Root, "env") }

// EnvNewDir is where a new environment is extracted to.
func (l RuntimeLayout) EnvNewDir() string { return filepath.Join(l.Root, "env.new") }

// EnvOldDir is where the previous environment waits during the swap.
func (l RuntimeLayout) EnvOldDir() string { return filepath.Join(l.Root, "env.old") }

// DownloadsDir holds the archive while it is downloaded and extracted.
func (l RuntimeLayout) DownloadsDir() string { return filepath.Join(l.Root, "downloads") }

// CacheDir is where the program's libraries cache their files (HF_HOME, TORCH_HOME, ...): below Root, so that nothing of the
// runtime lands in the user's profile.
func (l RuntimeLayout) CacheDir() string { return filepath.Join(l.Root, "cache") }

// TempDir is where the program writes temporary files (TEMP, TMP, TMPDIR), below Root for the same reason.
func (l RuntimeLayout) TempDir() string { return filepath.Join(l.Root, "tmp") }

// LogPath is the log of the runtime process.
func (l RuntimeLayout) LogPath() string { return filepath.Join(l.Root, "logs", "runtime.log") }

// StatePath is the record file.
func (l RuntimeLayout) StatePath() string { return filepath.Join(l.Root, "state.json") }

// CheckWritable creates Root if needed and makes sure a file can be created in it (ErrNotWritable otherwise).
func (l RuntimeLayout) CheckWritable() error { return checkWritable(l.Root) }

// RuntimeRecord is the installed runtime.
type RuntimeRecord struct {
	Version     string    `json:"version"`
	Variant     string    `json:"variant"`
	InstalledAt time.Time `json:"installed_at"`
}

// RuntimeState is the record of one runtime.
type RuntimeState struct {
	// AutoStart starts the runtime when mam starts (false: when it is first used).
	AutoStart bool `json:"auto_start"`
	// LastPort is the port the runtime was last started on; it is tried first.
	LastPort  int            `json:"last_port,omitempty"`
	Installed *RuntimeRecord `json:"installed,omitempty"`
	// Files are the files of the runtime's weights and dependencies (the catalog's assets) whose SHA-256 mam checked, by their path
	// below the environment directory: a file whose size and modification time still are these is not hashed again.
	Files map[string]FileStamp `json:"files,omitempty"`
}

// FileStamp is what a verified file looked like when it was verified.
type FileStamp struct {
	Size    int64 `json:"size"`
	ModTime int64 `json:"mod_time_unix_nano"`
}

func (s RuntimeState) clone() RuntimeState {
	if s.Installed != nil {
		r := *s.Installed
		s.Installed = &r
	}
	s.Files = maps.Clone(s.Files)
	return s
}

// RuntimeStore keeps the RuntimeState in RuntimeLayout.StatePath, written atomically.
type RuntimeStore struct {
	path string
	mu   sync.Mutex
	st   RuntimeState
}

// OpenRuntime reads the record (a missing file is the empty state). A record that cannot be parsed is moved aside to
// state.json.corrupt and the state starts empty, like Open: a damaged file never keeps mam from starting.
func OpenRuntime(l RuntimeLayout) (*RuntimeStore, error) {
	s := &RuntimeStore{path: l.StatePath()}
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	}
	if err := json.Unmarshal(data, &s.st); err != nil {
		s.st = RuntimeState{}
		if err := os.Rename(s.path, s.path+CorruptSuffix); err != nil {
			return nil, fmt.Errorf("move the unreadable %s aside: %w", s.path, err)
		}
	}
	return s, nil
}

// Get is a copy of the state.
func (s *RuntimeStore) Get() RuntimeState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.clone()
}

// Update changes the state with edit and writes it; nothing changes when the write fails.
func (s *RuntimeStore) Update(edit func(*RuntimeState)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.st.clone()
	edit(&next)
	data, err := json.MarshalIndent(next, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	if err := fsutil.WriteFileAtomic(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	s.st = next
	return nil
}
