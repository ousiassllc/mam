// Package llmlocal is the use case behind the card "ローカル LLM サーバー" of the category "テキスト生成（LLM）" of the settings screen and the
// MCP tools set_local_llm_server / test_local_llm_server (#1008): the address of the local OpenAI-compatible LLM server (Ollama, LM Studio, llama.cpp
// server), its optional API key, the connection test, and the switch that makes mam unload the Ollama models before a local image or video
// generation starts (spec/architecture/local-llm.md).
//
// The address is stored in the settings file of the LLM (llm.json, providers.local.base_url, the schema llm.Config reads) and handed to the running
// client at once (SetApplier). The key is stored in the credential store under the provider name "local", like the key of any provider; it is never
// returned, logged or put in an error.
package llmlocal

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/localllm"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
	"github.com/ousiassllc/mam/backend/internal/service/settingsfile"
)

// ErrPinned means the operator named the settings file (--llm-config / MAM_LLM_CONFIG): the screen does not write it.
var ErrPinned = errors.New("llmlocal: the settings file is pinned by a start option")

// InvalidError is a setting that cannot be saved. Message is shown to the user; nothing was written. Field is the form field to blame
// ("base_url" / "api_key"), when one is.
type InvalidError struct{ Message, Field string }

func (e *InvalidError) Error() string { return "invalid local LLM server setting: " + e.Message }

// KeyStoreError is a key that the OS secure storage could not keep (credential.StoreError): its message is for the user.
type KeyStoreError = credential.StoreError

// testTimeout bounds a connection test.
const testTimeout = 10 * time.Second

// maxListedModels is the most model ids a test result lists.
const maxListedModels = 20

// provider is the credential.Store key of the API key and the name of the provider in llm.json.
const provider = string(llm.ProviderLocal)

// Service reads and writes the settings of the local LLM server.
type Service struct {
	mu     sync.Mutex // one read-modify-write of the file at a time
	path   string
	pinned bool
	keys   credential.Store
	client httpclient.Doer
	apply  func(llm.Config) []string
	// managed is the llama.cpp server mam runs itself, ollama the Ollama server (#1105) (nil: none).
	managed, ollama Managed
}

// New returns a Service over the settings file at path (pinned: the screen must not write it) and the key store. keys may be nil (no key can
// be kept). client is the HTTP client of the test and the unload; nil selects the one of every registered endpoint (safefetch.NewEndpointClient).
func New(path string, pinned bool, keys credential.Store, client httpclient.Doer) *Service {
	if client == nil {
		client = safefetch.NewEndpointClient()
	}
	return &Service{path: path, pinned: pinned, keys: keys, client: client}
}

// SetApplier sets what a saved address is handed to: the running client (llm.Client.Apply), which takes the address at once. Without one a
// save only writes the file.
func (s *Service) SetApplier(apply func(llm.Config) []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apply = apply
}

// View is the saved settings.
type View struct {
	// BaseURL is the address the calls go to now (the saved one, else DefaultBaseURL).
	BaseURL        string
	DefaultBaseURL string
	// KeyConfigured is whether an API key is stored. KeyHint is its last characters (credential.Hint), "" without a key.
	KeyConfigured bool
	KeyHint       string
	// KeyUnreadable is true when the key store could not be read: the card cannot say whether a key is set.
	KeyUnreadable bool
	// UnloadBeforeGeneration is whether mam asks an Ollama server to unload its models before a local image or video generation.
	UnloadBeforeGeneration bool
	// UseManaged is whether the calls go to the llama.cpp server that mam installs and starts itself (the address above is then not used);
	// UseManagedOllama whether they go to the Ollama server that mam installs and starts itself (#1105). At most one is true. Use is the one they make.
	UseManaged, UseManagedOllama bool
	Use                          Use
	Pinned                       bool
	// Problem is why the settings file could not be read; the view then shows the defaults and a save is refused until it is repaired.
	Problem string
}

// View returns the saved settings.
func (s *Service) View() View {
	v := s.settingsView()
	if s.keys != nil {
		has, err := s.keys.Has(provider)
		v.KeyConfigured, v.KeyUnreadable = has, err != nil
		if has {
			v.KeyHint, _ = credential.Hint(s.keys, provider)
		}
	}
	return v
}

// settingsView is the View of the settings file alone (no key store is asked).
func (s *Service) settingsView() View {
	v := View{DefaultBaseURL: llm.LocalDefaultBaseURL, BaseURL: llm.LocalDefaultBaseURL, Pinned: s.pinned}
	if cfg, err := s.config(); err != nil {
		v.Problem = err.Error()
	} else {
		pc := cfg.Providers[llm.ProviderLocal]
		v.BaseURL, v.UnloadBeforeGeneration, v.UseManaged, v.UseManagedOllama = pc.BaseURL, pc.UnloadBeforeGeneration, pc.UseManagedRuntime, pc.UseManagedOllama
		v.Use = UseOf(pc.UseManagedRuntime, pc.UseManagedOllama)
	}
	return v
}

// config is the llm.Config the settings file makes.
func (s *Service) config() (llm.Config, error) {
	obj, err := settingsfile.Read(s.path)
	if err != nil {
		return llm.Config{}, err
	}
	data, err := settingsfile.Marshal(obj)
	if err != nil {
		return llm.Config{}, err
	}
	return llm.ParseConfig(data)
}

// Settings is the address now and whether the unload is on, for the callers that do not need the rest (the unload before a generation).
func (s *Service) Settings() (baseURL string, unload bool) {
	v := s.settingsView()
	return v.BaseURL, v.UnloadBeforeGeneration
}

// BaseURL is the address of the local server now. It is what the model list reads (llmmodels.Options.LocalBaseURL).
func (s *Service) BaseURL() string {
	u, _ := s.Settings()
	return u
}

// SaveInput is what a save changes. An empty BaseURL and a nil Unload keep what is saved; an empty APIKey keeps the stored key.
type SaveInput struct {
	BaseURL string
	// APIKey is the key to store (optional for a local server).
	APIKey string
	// ClearKey deletes the stored key (and wins over APIKey).
	ClearKey bool
	// Unload sets UnloadBeforeGeneration; nil keeps it.
	Unload *bool
	// UseManaged sets UseManaged (the llama.cpp server of mam instead of an address); nil keeps it. Choosing it with no such server in the app is ErrManagedUnavailable.
	UseManaged *bool
	// UseManagedOllama sets UseManagedOllama (the Ollama server of mam, #1105); nil keeps it. It is never true together with UseManaged: choosing one clears the other
	// (both true in one save is an *InvalidError). Choosing it with no such server in the app is ErrManagedOllamaUnavailable.
	UseManagedOllama *bool
}

// Save stores the address and the switch in llm.json (the rest of the file is kept; an address equal to the default is dropped, so that a later
// change of the default applies) and the key in the key store. Nothing is written when an input is invalid (*InvalidError), the file cannot be
// read as it is (*InvalidError), or it is pinned and the address or the switch is to change (ErrPinned; the key does not live in the file and may
// still be changed). A key store that refuses is a *KeyStoreError (the file is then already written).
func (s *Service) Save(in SaveInput) error {
	baseURL := strings.TrimSpace(in.BaseURL)
	if baseURL != "" {
		norm, err := localllm.NormalizeBaseURL(baseURL)
		if err != nil {
			return &InvalidError{Message: BaseURLProblem(err), Field: "base_url"}
		}
		baseURL = norm
	}
	key := ""
	if !in.ClearKey && in.APIKey != "" {
		clean, problem := credential.CleanAPIKey(in.APIKey)
		if problem != "" {
			return &InvalidError{Message: problem, Field: "api_key"}
		}
		key = clean
	}
	if (baseURL != "" || in.Unload != nil || in.UseManaged != nil || in.UseManagedOllama != nil) && s.pinned {
		return ErrPinned
	}
	useManaged, useOllama, err := exclusive(in.UseManaged, in.UseManagedOllama)
	if err != nil {
		return err
	}
	switch {
	case on(useManaged) && s.managedFor(UseLlamaCpp) == nil:
		return ErrManagedUnavailable
	case on(useOllama) && s.managedFor(UseOllama) == nil:
		return ErrManagedOllamaUnavailable
	}
	if (key != "" || in.ClearKey) && s.keys == nil {
		return &InvalidError{Message: "キーストアを利用できません", Field: "api_key"}
	}
	if e := (edit{baseURL: baseURL, unload: in.Unload, useManaged: useManaged, useOllama: useOllama}); e != (edit{}) {
		if err := s.write(e); err != nil {
			return err
		}
	}
	return s.saveKey(key, in.ClearKey)
}

func (s *Service) saveKey(key string, clear bool) error {
	switch {
	case clear:
		if err := s.keys.Delete(provider); err != nil && !errors.Is(err, credential.ErrNotFound) {
			return &KeyStoreError{Message: "OS のセキュアストレージから API キーを削除できませんでした", Err: err}
		}
	case key != "":
		err := credential.SetChecked(s.keys, provider, key)
		if errors.Is(err, credential.ErrTooLarge) {
			return &InvalidError{Message: "API キーが長すぎます", Field: "api_key"}
		}
		return err
	}
	return nil
}
