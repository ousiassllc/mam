// Package llmmodels is the use case behind the model picker of "LLM の割り当て" and the MCP tool list_llm_models: the
// models to offer for each provider. The list comes from the provider itself when it can be read (the API of the vendor with
// the saved API key, the models of the ChatGPT sign-in), else from the candidates mam ships (agent/llm/data/models.json,
// updated with the releases). Every answer says which of the two it is, so that a screen can tell the user.
//
// An answer is kept for ten minutes per provider and per account (the email of a sign-in, the fingerprint of an API key; a
// sign-in with no email is not kept), so a sign-out, another account or another key is never answered with the list of the
// one before. No token or key is returned, logged or put in an error.
package llmmodels

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/localllm"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/modelfilter"
)

// Source says where a list comes from.
type Source string

const (
	// SourceAPI is the list the provider gave (the account's own models).
	SourceAPI Source = "api"
	// SourceDefault is the candidates mam ships, shown when the provider's list could not be read.
	SourceDefault Source = "default"
)

// Fallback says why the candidates of mam are shown instead of the provider's list ("" for SourceAPI).
type Fallback string

const (
	// FallbackNone means the provider's own list is shown.
	FallbackNone Fallback = ""
	// FallbackNoKey means no API key is saved.
	FallbackNoKey Fallback = "no_key"
	// FallbackNotSignedIn means nobody is signed in with ChatGPT.
	FallbackNotSignedIn Fallback = "not_signed_in"
	// FallbackFailed means the provider did not answer, or answered something unusable.
	FallbackFailed Fallback = "failed"
	// FallbackLocalDown means the local server (llm.ProviderLocal) could not be read: it is not running, the base URL is wrong, or it refused the
	// request. Unlike a vendor's failure it makes the provider unusable (#1008): a local server that does not answer has no models.
	FallbackLocalDown Fallback = "local_unreachable"
)

// Message is why the candidates of mam are shown, as a short phrase for the user (empty for FallbackNone).
func (f Fallback) Message() string {
	switch f {
	case FallbackNoKey:
		return "API キーが登録されていません"
	case FallbackNotSignedIn:
		return "ChatGPT にログインしていません"
	case FallbackFailed:
		return "プロバイダーからモデルの一覧を取得できませんでした"
	case FallbackLocalDown:
		return "ローカルサーバーに接続できません"
	}
	return ""
}

// Model is one model to offer. Name is empty when the provider gave none (the candidates of mam have none).
type Model struct {
	ID   string
	Name string
}

// List is the models of one provider.
type List struct {
	Provider string
	Source   Source
	// Fallback is why Source is SourceDefault.
	Fallback Fallback
	Models   []Model
	// Availability is whether the provider can be used now: false without an API key or a sign-in (#1013), true otherwise (also when the list could not be
	// read: the key is there). A picker hides the models of a provider that cannot be used unless the user asks for them.
	Availability modelfilter.Availability
}

// Has is whether model is one of the listed models.
func (l List) Has(model string) bool {
	return slices.ContainsFunc(l.Models, func(m Model) bool { return m.ID == model })
}

// ErrUnknownProvider is a provider that is not one of llm.Providers().
var ErrUnknownProvider = errors.New("llmmodels: unknown provider")

// ChatGPT is what the two ChatGPT providers need of the sign-ins: who is signed in and the models they can use. A
// token never crosses this interface.
type ChatGPT interface {
	// Account identifies the signed-in account of the provider: a change of id drops the kept list. signedIn is false when
	// nobody is signed in; id is empty when somebody is but cannot be told from another account (then nothing is kept).
	Account(provider llm.Provider) (id string, signedIn bool)
	// Models are the models the account of the provider can use, in the server's order.
	Models(ctx context.Context, provider llm.Provider) ([]Model, error)
}

// Options build a Service. Everything is optional: without Keys the API-key providers show the candidates of mam, without
// ChatGPT the two ChatGPT providers do.
type Options struct {
	Keys    credential.Store
	ChatGPT ChatGPT
	// HTTP is the client of the vendors' model lists; nil selects httpclient.New().
	HTTP httpclient.Doer
	// BaseURLs replaces the API origin of a provider (default: llm.DefaultConfig()): the app gives the base_url of llm.json,
	// which is where the calls of the Agents go too, so an API key is never sent to another origin than the one the user set.
	BaseURLs map[llm.Provider]string
	// LocalBaseURL is the address of the local server (llm.ProviderLocal) now: the settings screen can change it while the app runs, so it is asked
	// at every read. Nil: BaseURLs, else the default.
	LocalBaseURL func() string
	// LocalManaged is the model of mam's own llama.cpp while the user has chosen it (managed true; see managed.go). Nil: never managed.
	LocalManaged func(ctx context.Context) (models []string, managed bool)
	// Logger receives why a list could not be read (never a token or a key); nil discards it.
	Logger *slog.Logger
	// Now replaces the clock (tests).
	Now func() time.Time
}

const (
	// TTL is how long a list the provider gave is kept.
	TTL = 10 * time.Minute
	// failureTTL is how long a failed read is kept (the candidates of mam are shown meanwhile): a provider that is down
	// is not asked at every opening of the picker.
	failureTTL = time.Minute
	// fetchTimeout bounds the read of one list.
	fetchTimeout = 10 * time.Second
)

type entry struct {
	account string
	at      time.Time
	ttl     time.Duration
	list    List
}

// Service lists the models of the providers.
type Service struct {
	o Options
	// localHTTP is the client of the local server's list: the one the caller gave, else (nil) the one of every registered endpoint.
	localHTTP httpclient.Doer
	cfg       llm.Config
	mu        sync.Mutex
	cached    map[llm.Provider]entry
}

// New builds the Service.
func New(o Options) *Service {
	localHTTP := o.HTTP
	if o.HTTP == nil {
		o.HTTP = httpclient.New()
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{o: o, localHTTP: localHTTP, cfg: llm.DefaultConfig(), cached: map[llm.Provider]entry{}}
}

// List is the models to offer for provider. It never fails because a provider could not be read: the candidates of mam
// answer then (List.Fallback says why). Only an unknown provider is an error (ErrUnknownProvider).
func (s *Service) List(ctx context.Context, provider string) (List, error) {
	l, err := s.list(ctx, provider)
	l.Availability = l.Fallback.availability()
	return l, err
}

func (s *Service) list(ctx context.Context, provider string) (List, error) {
	p := llm.Provider(provider)
	if !slices.Contains(llm.Providers(), p) {
		return List{}, fmt.Errorf("%w: %q", ErrUnknownProvider, provider)
	}
	account, fallback := s.account(p)
	if fallback != FallbackNone {
		return s.defaults(p, fallback), nil
	}
	// An empty account is a sign-in that cannot be told from another one (no email): its list is never kept.
	keep := account != ""
	if keep {
		s.mu.Lock()
		if e, ok := s.cached[p]; ok && e.account == account && s.o.Now().Sub(e.at) < e.ttl {
			s.mu.Unlock()
			return cloneList(e.list), nil
		}
		s.mu.Unlock()
	}

	list, err := s.fetch(ctx, p)
	e := entry{account: account, at: s.o.Now(), ttl: TTL, list: list}
	if err != nil {
		if ctx.Err() != nil {
			// The caller gave up (the picker was switched and htmx aborted the request): the provider did not fail, so
			// nothing is kept and the next opening reads the list again.
			return s.defaults(p, failedFallback(p)), nil
		}
		s.o.Logger.Warn("llm model list unavailable; showing the candidates of mam", "provider", p, "error", err)
		e.ttl, e.list = failureTTL, s.defaults(p, failedFallback(p))
	}
	if keep {
		s.mu.Lock()
		s.cached[p] = e
		s.mu.Unlock()
	}
	return cloneList(e.list), nil
}

// account is who the list of p is for: the fingerprint of the saved API key, the signed-in ChatGPT account (empty when a
// signed-in account cannot be told from another one). When there is none (no key, not signed in) it says why the
// candidates are shown.
func (s *Service) account(p llm.Provider) (string, Fallback) {
	switch p {
	case llm.ProviderChatGPTSubscription, llm.ProviderChatGPTCodex:
		if s.o.ChatGPT != nil {
			if id, signedIn := s.o.ChatGPT.Account(p); signedIn {
				return id, FallbackNone
			}
		}
		return "", FallbackNotSignedIn
	}
	if p == llm.ProviderLocal {
		// The key is optional and the address is a setting: a change of either is another account, so a list is never answered with the one
		// of the server before.
		key, _ := s.key(p)
		sum := sha256.Sum256([]byte(s.baseURL(p) + "\x00" + key + "\x00" + s.managedKey()))
		return hex.EncodeToString(sum[:8]), FallbackNone
	}
	key, ok := s.key(p)
	if !ok {
		return "", FallbackNoKey
	}
	sum := sha256.Sum256([]byte(key)) // the key itself is not kept
	return hex.EncodeToString(sum[:8]), FallbackNone
}

// failedFallback is why the candidates of mam are shown when the list of p could not be read.
func failedFallback(p llm.Provider) Fallback {
	if p == llm.ProviderLocal {
		return FallbackLocalDown
	}
	return FallbackFailed
}

func (s *Service) key(p llm.Provider) (string, bool) {
	if s.o.Keys == nil {
		return "", false
	}
	key, err := s.o.Keys.Get(string(p))
	return key, err == nil && key != ""
}

// fetch reads the list of p from the provider.
func (s *Service) fetch(ctx context.Context, p llm.Provider) (List, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	var models []Model
	if l, ok := s.managedList(ctx, p); ok {
		return l, nil
	}
	switch p {
	case llm.ProviderChatGPTSubscription, llm.ProviderChatGPTCodex:
		m, err := s.o.ChatGPT.Models(ctx, p)
		if err != nil {
			return List{}, err
		}
		models = m
	default:
		key, ok := s.key(p)
		if !ok && p != llm.ProviderLocal { // a local server needs no key
			return List{}, credential.ErrNotFound
		}
		ids, err := s.fetchIDs(ctx, p, key)
		if err != nil {
			return List{}, err
		}
		for _, id := range ids {
			models = append(models, Model{ID: id})
		}
	}
	if len(models) == 0 && p != llm.ProviderLocal { // a local server with no model pulled yet is connected: the list is empty, not failed
		return List{}, errors.New("llmmodels: the provider listed no model")
	}
	return List{Provider: string(p), Source: SourceAPI, Models: models}, nil
}

// fetchIDs reads the ids of the models of an API-key provider, or of the local server (which has the client of every registered endpoint, not the
// vendors' one, unless the caller gave one).
func (s *Service) fetchIDs(ctx context.Context, p llm.Provider, key string) ([]string, error) {
	if p == llm.ProviderLocal {
		return localllm.ListModels(ctx, s.localHTTP, s.baseURL(p), key)
	}
	return llm.ListModels(ctx, s.o.HTTP, p, s.baseURL(p), key)
}

func (s *Service) baseURL(p llm.Provider) string {
	if p == llm.ProviderLocal && s.o.LocalBaseURL != nil {
		if u := s.o.LocalBaseURL(); u != "" {
			return u
		}
	}
	if u := s.o.BaseURLs[p]; u != "" {
		return u
	}
	return s.cfg.Providers[p].BaseURL
}

// defaults is the candidates of mam.
func (s *Service) defaults(p llm.Provider, why Fallback) List {
	l := List{Provider: string(p), Source: SourceDefault, Fallback: why}
	for _, id := range llm.CandidateModels(p) {
		l.Models = append(l.Models, Model{ID: id})
	}
	return l
}

func cloneList(l List) List {
	l.Models = slices.Clone(l.Models)
	return l
}
