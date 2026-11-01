package localruntime

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
)

// VariantView is a build of the runtime with what it needs.
type VariantView struct {
	Variant       catalog.Variant `json:"variant"`
	Label         string          `json:"label"`
	Size          int64           `json:"size"`
	ExtractedSize int64           `json:"extracted_size"`
	// NeedBytes is the free space the install needs (what is not downloaded yet and the extracted files).
	NeedBytes int64 `json:"need_bytes"`
}

// View is a runtime: what it is, whether this machine can run it, what is installed against what is pinned, and its process.
type View struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Purpose     catalog.Purpose `json:"purpose"`
	Description string          `json:"description"`
	SourceURL   string          `json:"source_url"`
	// License is the license of the runtime's program (and what is not clear about the rest of the package: Notice).
	License catalog.License `json:"license"`
	VRAM    catalog.VRAM    `json:"vram"`
	// Support says whether the machine can run it; a refusal has the reason and the cloud provider to use instead.
	Support rtm.Support `json:"support"`
	rtm.Status
	Process   proc.Status `json:"process"`
	AutoStart bool        `json:"auto_start"`
	// Dir is the directory below localai that holds the runtime; Writable is false (with WritableError) when mam.exe is in a
	// place the user cannot write to. Neither is probed (nothing is created) on a machine that cannot run the runtime.
	Dir           string `json:"dir"`
	Writable      bool   `json:"writable"`
	WritableError string `json:"writable_error,omitempty"`
	FreeBytes     uint64 `json:"free_bytes"`
	// LogTail is the end of the runtime's log ("" for none), to see why a start failed or what it is doing.
	LogTail  string        `json:"log_tail,omitempty"`
	Variants []VariantView `json:"variants"`
	// InstallJob is the install that is queued or running ("" for none).
	InstallJob string `json:"install_job,omitempty"`
	// Weights are the weights and dependencies the runtime needs, with where each comes from and its license.
	Weights WeightsView `json:"weights"`
}

// List describes every runtime of the catalog.
func (s *Service) List(ctx context.Context) []View {
	views := make([]View, 0, len(s.instances))
	for _, inst := range s.instances {
		views = append(views, s.view(ctx, inst))
	}
	return views
}

// Status describes the runtime with the id (ErrNotFound for another).
func (s *Service) Status(ctx context.Context, id string) (View, error) {
	inst, err := s.instance(id)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, inst), nil
}

func (s *Service) view(ctx context.Context, inst *instance) View {
	rt := inst.rt
	v := View{
		ID: rt.ID, Name: rt.Name, Purpose: rt.Purpose, Description: rt.Description, SourceURL: rt.SourceURL, License: rt.License, VRAM: rt.VRAM,
		Support: s.support(ctx, inst, false), Status: inst.install.Status(), Process: inst.proc.Status(), AutoStart: inst.state.Get().AutoStart,
		Dir: inst.layout.Root, LogTail: tailOf(inst.layout.LogPath()), InstallJob: s.activeJob(TargetPrefix + rt.ID), Variants: []VariantView{},
	}
	if v.Support.Supported {
		if err := inst.layout.CheckWritable(); err != nil {
			v.WritableError = err.Error()
		} else {
			v.Writable = true
		}
	}
	v.Weights = weightsView(inst, v.Installed)
	v.FreeBytes = s.free(inst.layout.Root)
	for _, name := range rt.VariantList() {
		a := rt.Variants[name]
		need, _ := inst.install.NeedBytes(name)
		v.Variants = append(v.Variants, VariantView{Variant: name, Label: a.Label, Size: a.Size, ExtractedSize: a.ExtractedSize, NeedBytes: need})
	}
	return v
}

func (s *Service) free(path string) uint64 {
	f := s.FreeBytes
	if f == nil {
		f = fsutil.FreeBytes
	}
	n, err := f(path)
	if err != nil {
		return 0
	}
	return n
}

func (s *Service) activeJob(target string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active[target]
}

// logTailBytes is how much of the end of the log a View carries.
const logTailBytes = 4096

// tailOf is the last bytes of the file at path as text ("" when it cannot be read).
func tailOf(path string) string {
	f, err := os.Open(path) // #nosec G304 -- the runtime's own log path
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err == nil && st.Size() > logTailBytes {
		if _, err := f.Seek(-logTailBytes, io.SeekEnd); err != nil {
			return ""
		}
	}
	b, _ := io.ReadAll(f)
	return strings.TrimSpace(strings.ToValidUTF8(string(b), ""))
}
