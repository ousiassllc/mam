package ollamapull

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/ollamamodels"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// State is where a model stands.
type State string

// The states of a model of the list.
const (
	// StateMissing: not fetched. StatePulling: a pull is queued or running. StateInstalled: fetched, and the manifest is the recorded one. StateChanged: fetched, but the manifest differs
	// from the record (the tag was moved): it works, and is not deleted. StateUnavailable: the Ollama of mam is not installed here or cannot run on this machine.
	StateMissing     State = "missing"
	StatePulling     State = "pulling"
	StateInstalled   State = "installed"
	StateChanged     State = "changed"
	StateUnavailable State = "unavailable"
)

// ModelView is a model of the list with where it stands.
type ModelView struct {
	Model ollamamodels.Model
	State State
	// Check is the result of the comparison of the manifest (CheckMissing while it is not fetched).
	Check Check
	// PullJob is the pull that is queued or running ("" for none).
	PullJob string
	// NoSpace says the disk of the models has not the room a pull needs (the model and 1 GiB): it is not fetched, and a pull is refused.
	NoSpace bool
}

// Other is a model the Ollama of mam holds that is not in the list (the user fetched it with their own ollama): its name and size, nothing more.
type Other struct {
	Name string
	Size int64
}

// Server is where the Ollama of mam stands, for the line of the section.
type Server string

// The states of the server.
const (
	// ServerUnsupported: this machine cannot run it. ServerNotInstalled: it is not installed. ServerInstalling: an install runs. ServerStopped: installed, and it starts with the first pull or call.
	// ServerStarting, ServerRunning, ServerFailed: its process.
	ServerUnsupported  Server = "unsupported"
	ServerNotInstalled Server = "not_installed"
	ServerInstalling   Server = "installing"
	ServerStopped      Server = "stopped"
	ServerStarting     Server = "starting"
	ServerRunning      Server = "running"
	ServerFailed       Server = "failed"
)

// View is the section: the server, the models of the list, and the models of the server that are not in the list.
type View struct {
	Server Server
	// Reason says why the server cannot be used (the reason of the machine, the end of the failure): "" when it can.
	Reason string
	Models []ModelView
	// Others are the models the running server holds that are not in the list (empty while it does not run).
	Others []Other
	// ModelsDir is where the models are kept; FreeBytes the free space of its disk.
	ModelsDir string
	FreeBytes uint64
}

// Usable says the server is installed and can run here: a pull may be asked (it starts the server when it is stopped).
func (v View) Usable() bool {
	return v.Server == ServerStopped || v.Server == ServerStarting || v.Server == ServerRunning || v.Server == ServerFailed
}

// serverOf is where the server stands from the view of its runtime.
func serverOf(v localruntime.View) (Server, string) {
	switch {
	case !v.Support.Supported:
		return ServerUnsupported, v.Support.Message()
	case v.InstallJob != "":
		return ServerInstalling, ""
	case !v.Installed:
		return ServerNotInstalled, ""
	}
	switch v.Process.State {
	case proc.StateRunning:
		return ServerRunning, ""
	case proc.StateStarting:
		return ServerStarting, ""
	case proc.StateFailed:
		return ServerFailed, v.Process.Error
	}
	return ServerStopped, ""
}

// List reads the section now: the state of each model of the list from its manifest on disk (so a stopped server shows what it holds), and the models of a running server that are not
// in the list from its /api/tags. A server that cannot be asked (not installed, stopped, not answering) shows no others.
func (s *Service) List(ctx context.Context) View {
	rv, err := s.Runtime.Status(ctx, RuntimeID)
	if err != nil {
		return View{Server: ServerNotInstalled, Reason: err.Error()}
	}
	v := View{ModelsDir: s.layout.ModelsDir(), FreeBytes: rv.FreeBytes}
	if free, err := s.free(); err == nil {
		v.FreeBytes = free
	}
	v.Server, v.Reason = serverOf(rv)
	for _, m := range s.Models {
		mv := ModelView{Model: m, State: StateUnavailable, PullJob: s.activeJob(TargetPrefix + m.Name)}
		if v.Server == ServerUnsupported || v.Server == ServerNotInstalled {
			v.Models = append(v.Models, mv)
			continue
		}
		mv.Check = s.CheckManifest(m)
		mv.NoSpace = v.FreeBytes < uint64(m.TotalBytes)+spaceMargin //nolint:gosec // a validated size
		switch {
		case mv.PullJob != "":
			mv.State = StatePulling
		case mv.Check == CheckVerified:
			mv.State = StateInstalled
		case mv.Check == CheckChanged:
			mv.State = StateChanged
		default:
			mv.State = StateMissing
		}
		v.Models = append(v.Models, mv)
	}
	if v.Server == ServerRunning {
		v.Others = s.others(ctx, processURL(rv))
	}
	return v
}

// tagsTimeout bounds the question to the running server for its models: the section is drawn while it answers.
const tagsTimeout = 3 * time.Second

// tag is a model of GET /api/tags.
type tag struct {
	Name  string `json:"name"`
	Model string `json:"model"`
	Size  int64  `json:"size"`
}

// tags asks the running server which models it holds (GET /api/tags). A server that does not answer is an error.
func (s *Service) tags(ctx context.Context, baseURL string) ([]tag, error) {
	ctx, cancel := context.WithTimeout(ctx, tagsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", http.NoBody)
	if err != nil {
		return nil, err
	}
	res, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("ollama: /api/tags answered " + res.Status)
	}
	var out struct {
		Models []tag `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

func (t tag) name() string {
	if t.Model != "" {
		return t.Model
	}
	return t.Name
}

// others are the models of the running server that are not in the list, by name.
func (s *Service) others(ctx context.Context, baseURL string) []Other {
	tags, err := s.tags(ctx, baseURL)
	if err != nil {
		return nil
	}
	var out []Other
	for _, t := range tags {
		if _, ok := ollamamodels.Find(s.Models, t.name()); !ok {
			out = append(out, Other{Name: t.name(), Size: t.Size})
		}
	}
	slices.SortFunc(out, func(a, b Other) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

// Names are the models the Ollama of mam answers to: the ones it holds when it runs (GET /api/tags), else the models of the list whose manifest is on disk. The model list of the LLM
// assignment shows them. It never starts the server.
func (s *Service) Names(ctx context.Context) []string {
	rv, err := s.Runtime.Status(ctx, RuntimeID)
	if err != nil || !rv.Support.Supported || !rv.Installed {
		return nil
	}
	if url := processURL(rv); url != "" {
		if tags, err := s.tags(ctx, url); err == nil {
			names := make([]string, 0, len(tags))
			for _, t := range tags {
				names = append(names, t.name())
			}
			slices.Sort(names)
			return names
		}
	}
	var names []string
	for _, m := range s.Models {
		if s.CheckManifest(m) != CheckMissing {
			names = append(names, m.Name)
		}
	}
	return names
}
