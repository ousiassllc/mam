// Package loraimport takes a LoRA file the user picked on their own PC into the managed loras folder (Issue #1119; spec
// model-manager.md §14). It is a local copy, not a download and not a publication: nothing leaves the PC and nothing is generated.
//
// An import has two steps, so that the user sees what the file is before it is placed. Stage streams the upload into the imports
// folder (below the same root, so that the last step is a rename), computing its SHA-256, and Inspect-checks it (lorafile): the name, the
// header, the size, the tensors of a LoRA. Commit then takes the metadata the user confirmed, refuses a name that is taken (a file is never
// overwritten; the refusal suggests another name) and renames the staged file into data/models/loras/ in one step. The record in state.json
// (store.LocalLoRA) holds what the user confirmed. Import is both steps in one call, for the MCP upload purpose.
//
// It is a package of its own because modelmanager is at its size limit; the Service holds one (Service.LoRAs) and passes what it owns:
// the layout, the record, the lock of a destination and the free space.
package loraimport

import (
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// ErrNotFound says there is no LoRA of the user at that path.
var ErrNotFound = errors.New("loraimport: not found")

const (
	// Folder is the folder below data/models/ the files go to.
	Folder = "loras"
	// staleAfter is how long a checked file waits for the user's confirmation before it is thrown away.
	staleAfter = time.Hour
	// maxStaged bounds the files that wait at once (each may be gigabytes).
	maxStaged = 3
)

// Deps are what the importer needs from the Service.
type Deps struct {
	Layout store.Layout
	State  *store.Store
	Now    func() time.Time
	// Free is the free bytes at a path (0 when it cannot be read).
	Free func(path string) uint64
	// Supported is false where the bundled engine does not exist: its models folder is the destination, so nothing is taken in.
	Supported bool
	// MaxBytes is the largest file taken in (0: lorafile.MaxBytes). A test sets a small one.
	MaxBytes int64
	// Lock takes the lock of the destinations (paths below the data directory) and returns the unlock (Service.lockDests).
	Lock func(rels ...string) func()
}

// Service is the importer.
type Service struct {
	Deps
	mu     sync.Mutex
	staged map[string]*entry
}

type entry struct {
	Staged
	file    string
	created time.Time
}

// New returns the importer and throws away what an earlier run left in the imports folder.
func New(d Deps) *Service {
	if d.Layout.Root != "" {
		_ = os.RemoveAll(d.Layout.ImportsDir())
	}
	return &Service{Deps: d, staged: map[string]*entry{}}
}

// Available says a LoRA file can be added: the bundled engine exists on this OS, so its loras folder is where the file goes.
func (s *Service) Available() bool { return s.Supported }

// Conflict says the name is taken in the loras folder. Same: the file there has the same content. Suggest is a name that is free.
type Conflict struct {
	Name    string
	Same    bool
	Suggest string
}

// Staged is a file that was checked and waits for the user: what it is, what it says of itself, and whether its name is taken.
type Staged struct {
	Token        string
	OriginalName string
	// Name is the file name it would get; Renamed says it differs from OriginalName (characters were replaced).
	Name    string
	Renamed bool
	Size    int64
	SHA256  string
	Meta    lorafile.Metadata
	// Conflict is set when Name is taken.
	Conflict *Conflict
}

// Meta is what the user confirmed about a LoRA. The zero value is "nothing set": the screens say 未設定 and 不明.
type Meta struct {
	// BaseModels are lorafamily keys.
	BaseModels []string
	// Strength is the recommended strength in (0, 1]; nil: not set.
	Strength *float64
	// TriggerWords is the text the user typed (comma or newline separated).
	TriggerWords string
	// LicenseName is optional free text; Commercial is "" (unknown), "conditional" (the user declares commercial use is allowed) or "no".
	LicenseName, Commercial string
}

// Input is a Commit: the staged file, the name to give it ("" keeps Staged.Name) and the metadata.
type Input struct {
	Token, Name string
	Meta
}

// Model is a LoRA the user added, as the list shows it.
type Model struct {
	store.LocalLoRA
	// Present is false when the file is gone from the folder (the record stays until the user removes it).
	Present bool `json:"present"`
}

func invalid(format string, args ...any) error {
	return &modelsettings.InvalidError{Message: fmt.Sprintf(format, args...)}
}

// refusal turns what lorafile says into a refusal the screens show as it is.
func refusal(err error) error {
	var r *lorafile.Reject
	if errors.As(err, &r) {
		return invalid("%s", r.Message)
	}
	return err
}

func (s *Service) lorasDir() string { return s.Layout.ModelPath(path.Join("models", Folder)) }

func relOf(name string) string { return path.Join("models", Folder, name) }
