// Package llm is the Reasoning LLM Adapter: the layer through which every Agent calls a
// language model (spec/architecture/model-adapters.md §2). Which provider and model an
// Agent uses comes from Config, never from the Agent's code. Calls are retried with
// exponential backoff on timeouts and 429, each provider has a circuit breaker, and a call
// is routed to the Agent's alternative provider when the primary one fails or its
// breaker is open (non-functional.md §6, model-adapters.md §7).
package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Agent names an Agent that reasons with an LLM. The set follows the table of
// model-adapters.md §2.
type Agent string

// Agents.
const (
	AgentOrchestrator       Agent = "orchestrator"
	AgentProducer           Agent = "producer"
	AgentWriter             Agent = "writer"
	AgentDirector           Agent = "director"
	AgentStoryboard         Agent = "storyboard"
	AgentArtDirector        Agent = "art_director"
	AgentPromptPlanner      Agent = "prompt_planner"
	AgentVideoJudge         Agent = "video_judge"
	AgentRepair             Agent = "repair"
	AgentEditor             Agent = "editor"
	AgentAudioDirector      Agent = "audio_director"
	AgentCommandInterpreter Agent = "command_interpreter"
	AgentStateExtractor     Agent = "state_extractor"
	AgentFinalQC            Agent = "final_qc"
)

// Provider names an LLM vendor. It is also the credential.Store key of the vendor's API key.
type Provider string

// Providers.
const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderGoogle    Provider = "google"
)

// Providers lists every supported provider.
func Providers() []Provider {
	return []Provider{ProviderAnthropic, ProviderOpenAI, ProviderGoogle}
}

// Target is one provider and the model to call on it.
type Target struct {
	Provider Provider `json:"provider"`
	Model    string   `json:"model"`
}

// Route is the provider assignment of one Agent: the primary target and the alternative
// used when the primary fails or its circuit breaker is open.
type Route struct {
	Primary  Target `json:"primary"`
	Fallback Target `json:"fallback"`
}

// ProviderConfig holds the connection settings of a provider.
type ProviderConfig struct {
	// BaseURL is the API origin, e.g. "https://api.anthropic.com".
	BaseURL string `json:"base_url"`
}

// RetryConfig is the exponential backoff of one target: a failed attempt is repeated after
// BaseDelay, 2*BaseDelay, 4*BaseDelay ... (at most MaxDelay), MaxAttempts calls in total.
type RetryConfig struct {
	MaxAttempts     int `json:"max_attempts"`
	BaseDelayMillis int `json:"base_delay_ms"`
	MaxDelayMillis  int `json:"max_delay_ms"`
}

// BreakerConfig is the per-provider circuit breaker: after FailureThreshold consecutive
// failed calls the provider is skipped for OpenSeconds, then one probe call is allowed.
type BreakerConfig struct {
	FailureThreshold int `json:"failure_threshold"`
	OpenSeconds      int `json:"open_seconds"`
}

// Config is the Reasoning LLM Adapter setting. DefaultConfig holds the initial assignment
// of model-adapters.md §2; a JSON file (LoadConfig) overrides any part of it, so the
// assignment can follow benchmark results without a code change.
type Config struct {
	Routes    map[Agent]Route             `json:"routes"`
	Providers map[Provider]ProviderConfig `json:"providers"`
	// TimeoutSeconds bounds one call of a provider.
	TimeoutSeconds int           `json:"timeout_seconds"`
	Retry          RetryConfig   `json:"retry"`
	Breaker        BreakerConfig `json:"breaker"`
}

// Timeout is the limit of one provider call.
func (c Config) Timeout() time.Duration { return time.Duration(c.TimeoutSeconds) * time.Second }

func (c Config) baseDelay() time.Duration {
	return time.Duration(c.Retry.BaseDelayMillis) * time.Millisecond
}

func (c Config) maxDelay() time.Duration {
	return time.Duration(c.Retry.MaxDelayMillis) * time.Millisecond
}

func (c Config) breakerOpen() time.Duration {
	return time.Duration(c.Breaker.OpenSeconds) * time.Second
}

// Default model of each provider. Model names change quickly (model-adapters.md §8), which
// is why routes are configuration.
const (
	defaultClaude = "claude-opus-4-5"
	defaultGPT    = "gpt-5"
	defaultGemini = "gemini-3.1-pro"
)

func claude() Target { return Target{ProviderAnthropic, defaultClaude} }
func gpt() Target    { return Target{ProviderOpenAI, defaultGPT} }
func gemini() Target { return Target{ProviderGoogle, defaultGemini} }

// DefaultConfig is the initial assignment of model-adapters.md §2.
func DefaultConfig() Config {
	return Config{
		Routes: map[Agent]Route{
			AgentOrchestrator:       {claude(), gpt()},
			AgentProducer:           {gpt(), claude()},
			AgentWriter:             {gpt(), claude()},
			AgentDirector:           {claude(), gemini()},
			AgentStoryboard:         {claude(), gemini()},
			AgentArtDirector:        {gemini(), gpt()},
			AgentPromptPlanner:      {claude(), gemini()},
			AgentVideoJudge:         {gemini(), gpt()},
			AgentRepair:             {claude(), gpt()},
			AgentEditor:             {gemini(), claude()},
			AgentAudioDirector:      {gpt(), claude()},
			AgentCommandInterpreter: {claude(), gpt()},
			AgentStateExtractor:     {gemini(), claude()},
			AgentFinalQC:            {claude(), gemini()},
		},
		Providers: map[Provider]ProviderConfig{
			ProviderAnthropic: {BaseURL: "https://api.anthropic.com"},
			ProviderOpenAI:    {BaseURL: "https://api.openai.com"},
			ProviderGoogle:    {BaseURL: "https://generativelanguage.googleapis.com"},
		},
		TimeoutSeconds: 120,
		Retry:          RetryConfig{MaxAttempts: 3, BaseDelayMillis: 1000, MaxDelayMillis: 16000},
		Breaker:        BreakerConfig{FailureThreshold: 5, OpenSeconds: 30},
	}
}

// LoadConfig returns DefaultConfig with the settings of the JSON file applied on top. An
// empty path selects the defaults. Unknown keys are rejected so a typo does not silently
// leave a route unchanged.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator chooses the settings file
	if err != nil {
		return Config{}, fmt.Errorf("read llm config: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode llm config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("llm config %s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks that every target names a known provider and a model, every provider
// has a base URL and the numeric settings are positive.
func (c Config) Validate() error {
	var errs []error
	for agent, r := range c.Routes {
		for role, t := range map[string]Target{"primary": r.Primary, "fallback": r.Fallback} {
			if _, ok := c.Providers[t.Provider]; !ok {
				errs = append(errs, fmt.Errorf("route %s %s: unknown provider %q", agent, role, t.Provider))
			}
			if t.Model == "" {
				errs = append(errs, fmt.Errorf("route %s %s: model is empty", agent, role))
			}
		}
	}
	for p, pc := range c.Providers {
		if pc.BaseURL == "" {
			errs = append(errs, fmt.Errorf("provider %s: base_url is empty", p))
		}
	}
	if c.TimeoutSeconds <= 0 || c.Retry.MaxAttempts <= 0 || c.Retry.BaseDelayMillis <= 0 ||
		c.Retry.MaxDelayMillis < c.Retry.BaseDelayMillis || c.Breaker.FailureThreshold <= 0 || c.Breaker.OpenSeconds <= 0 {
		errs = append(errs, errors.New("timeout, retry and breaker settings must be positive (max_delay_ms >= base_delay_ms)"))
	}
	return errors.Join(errs...)
}

// RouteFor resolves the providers of the Agent from the setting.
func (c Config) RouteFor(agent Agent) (Route, error) {
	r, ok := c.Routes[agent]
	if !ok {
		return Route{}, fmt.Errorf("llm: no route configured for agent %q", agent)
	}
	return r, nil
}
