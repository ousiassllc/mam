// Package llmassign is the use case behind the "LLM の割り当て" section of the settings screen: which
// provider and model each Agent calls first (primary) and falls back to (fallback). The assignment is
// stored in the settings file of the data directory (config.Config.LLMSettingsFile, llm.json) as the
// overrides of the defaults, in the schema llm.LoadConfig reads, so it is in effect from the next start.
//
// The package does not import service (service holds it in Services), so it has its own errors.
package llmassign

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// MaxModelBytes bounds a model name (the longest ones are well under 100 bytes).
const MaxModelBytes = 128

// ErrPinned means the operator named the settings file (--llm-config / MAM_LLM_CONFIG): the screen
// does not write it.
var ErrPinned = errors.New("llmassign: the settings file is pinned by a start option")

// InvalidError is an assignment that cannot be saved. Message is shown to the user; nothing was written.
type InvalidError struct{ Message string }

func (e *InvalidError) Error() string { return "invalid LLM assignment: " + e.Message }

// labels are the Japanese names of the Agents.
var labels = map[llm.Agent]string{
	llm.AgentOrchestrator:       "Orchestrator（進行管理）",
	llm.AgentProducer:           "Producer（コンセプト）",
	llm.AgentWriter:             "Writer（脚本）",
	llm.AgentDirector:           "Director（Scene / Shot の計画）",
	llm.AgentStoryboard:         "Storyboard（絵コンテ）",
	llm.AgentArtDirector:        "Art Director（Reference Pack）",
	llm.AgentPromptPlanner:      "Prompt Planner（Shot Spec）",
	llm.AgentVideoJudge:         "AI Video Judge（動画の評価）",
	llm.AgentRepair:             "Repair（修復戦略の選択）",
	llm.AgentEditor:             "Editor（編集の判定）",
	llm.AgentAudioDirector:      "Audio Director（音声の設計）",
	llm.AgentCommandInterpreter: "Command Interpreter（DIRECTOR 指示）",
	llm.AgentStateExtractor:     "State Extractor（World State の抽出）",
	llm.AgentFinalQC:            "Final QC（最終検査）",
}

// unused are the Agents whose route no code reads today (spec/architecture/model-adapters.md §2): the
// Orchestrator advances the state machine deterministically and Final QC only runs deterministic checks.
// Their routes stay valid and editable, so that a later use of them finds the assignment in place.
var unused = []llm.Agent{llm.AgentOrchestrator, llm.AgentFinalQC}

// Target is one provider and one model name.
type Target struct{ Provider, Model string }

// Route is the primary target of an Agent and the fallback it switches to when the primary fails.
type Route struct{ Primary, Fallback Target }

// Entry is the assignment of one Agent (Agent is its name in the settings file): what is saved (Route) and
// what the program ships (Default).
type Entry struct {
	Agent   string
	Label   string
	Unused  bool
	Route   Route
	Default Route
}

func routeOf(r llm.Route) Route {
	return Route{Primary: Target{string(r.Primary.Provider), r.Primary.Model}, Fallback: Target{string(r.Fallback.Provider), r.Fallback.Model}}
}

func (r Route) llm() llm.Route {
	return llm.Route{
		Primary:  llm.Target{Provider: llm.Provider(r.Primary.Provider), Model: r.Primary.Model},
		Fallback: llm.Target{Provider: llm.Provider(r.Fallback.Provider), Model: r.Fallback.Model},
	}
}

// View is the saved assignment, in the order of llm.Agents(). Problem is why the settings file could not be
// read (a file edited by hand into something llm.LoadConfig refuses): the Entries then show the defaults
// and a save is refused until the file is repaired. Pinned is the file the operator named; the screen only
// shows it. Providers are the choices of a provider.
type View struct {
	Entries   []Entry
	Providers []string
	Pinned    bool
	Problem   string
}

// Service reads and writes the assignment of the Agents.
type Service struct {
	mu     sync.Mutex // one read-modify-write of the file at a time
	path   string
	pinned bool
}

// New returns a Service over the settings file at path; pinned means the screen must not write it.
func New(path string, pinned bool) *Service { return &Service{path: path, pinned: pinned} }

// View returns the assignment that is saved in the settings file (defaults where it has no override),
// which is what the next start runs with, not what runs now.
func (s *Service) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	def := llm.DefaultConfig()
	cfg, problem := def, ""
	if obj, err := settingsfile.Read(s.path); err != nil {
		problem = err.Error()
	} else if data, err := settingsfile.Marshal(obj); err != nil {
		problem = err.Error()
	} else if cfg, err = llm.ParseConfig(data); err != nil {
		cfg, problem = def, err.Error()
	}
	v := View{Providers: providers(), Pinned: s.pinned, Problem: problem}
	for _, a := range llm.Agents() {
		v.Entries = append(v.Entries, Entry{
			Agent: string(a), Label: labels[a], Unused: slices.Contains(unused, a), Route: routeOf(cfg.Routes[a]), Default: routeOf(def.Routes[a]),
		})
	}
	return v
}

func providers() []string {
	var out []string
	for _, p := range llm.Providers() {
		out = append(out, string(p))
	}
	return out
}

// Save stores the routes of the Agents in routes: a route equal to the default is dropped from the file, so a
// later change of the default still applies, and everything else in the file (other Agents, base URLs, the
// retry policy) is kept as it is. An Agent that is missing from routes keeps what the file says. Nothing is
// written when a route is invalid or the result would not load (*InvalidError), or when the file is pinned
// (ErrPinned).
func (s *Service) Save(routes map[string]Route) error {
	def := llm.DefaultConfig().Routes
	for name, r := range routes {
		if _, ok := def[llm.Agent(name)]; !ok {
			return &InvalidError{Message: fmt.Sprintf("%q は割り当てを変えられる Agent ではありません", name)}
		}
		if err := checkRoute(llm.Agent(name), r.llm()); err != nil {
			return err
		}
	}
	return s.update(func(saved map[string]json.RawMessage) error {
		for name, r := range routes {
			if r.llm() == def[llm.Agent(name)] {
				delete(saved, name)
				continue
			}
			raw, err := json.Marshal(r.llm())
			if err != nil {
				return fmt.Errorf("encode route: %w", err)
			}
			saved[name] = raw
		}
		return nil
	})
}

// Agents are the names of the Agents whose route the screen edits, in display order.
func Agents() []string {
	var out []string
	for _, a := range llm.Agents() {
		out = append(out, string(a))
	}
	return out
}

// Reset drops every override of the Agents' routes, so that the defaults apply from the next start. The
// rest of the file is kept. ErrPinned when the file is pinned.
func (s *Service) Reset() error {
	return s.update(func(saved map[string]json.RawMessage) error {
		for _, a := range llm.Agents() {
			delete(saved, string(a))
		}
		return nil
	})
}

// update applies edit to the "routes" object of the settings file, checks that the result loads, and writes it.
func (s *Service) update(edit func(routes map[string]json.RawMessage) error) error {
	if s.pinned {
		return ErrPinned
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return &InvalidError{Message: "設定ファイル " + s.path + " を読み込めません。直すか削除してからやり直してください（" + err.Error() + "）"}
	}
	routes := map[string]json.RawMessage{}
	if raw, ok := obj["routes"]; ok {
		if err := json.Unmarshal(raw, &routes); err != nil || routes == nil {
			return &InvalidError{Message: "設定ファイル " + s.path + " の routes が読み込めません。直すか削除してからやり直してください"}
		}
	}
	if err := edit(routes); err != nil {
		return err
	}
	if len(routes) == 0 {
		delete(obj, "routes")
	} else if obj["routes"], err = json.Marshal(routes); err != nil {
		return fmt.Errorf("encode routes: %w", err)
	}
	data, err := settingsfile.Marshal(obj)
	if err != nil {
		return err
	}
	if _, err := llm.ParseConfig(data); err != nil {
		return &InvalidError{Message: "この割り当ては保存できません: " + err.Error()}
	}
	return settingsfile.Write(s.path, obj)
}

func checkRoute(a llm.Agent, r llm.Route) error {
	for _, t := range []struct {
		name   string
		target llm.Target
	}{{"プライマリ", r.Primary}, {"代替", r.Fallback}} {
		where := labels[a] + " の" + t.name
		switch model := strings.TrimSpace(t.target.Model); {
		case !slices.Contains(llm.Providers(), t.target.Provider):
			return &InvalidError{Message: where + ": 未対応のプロバイダーです"}
		case model == "":
			return &InvalidError{Message: where + ": モデル名を入力してください"}
		case model != t.target.Model || strings.ContainsAny(model, " \t\r\n"):
			return &InvalidError{Message: where + ": モデル名に空白や改行は含められません"}
		case len(model) > MaxModelBytes:
			return &InvalidError{Message: where + ": モデル名が長すぎます"}
		}
	}
	return nil
}
