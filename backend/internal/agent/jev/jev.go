// Package jev is the client of TypeSafe AI Jev (System One API, POST /v1/systemone), the decision
// model the Repair Agent runs beside its own LLM decision (spec/architecture/model-adapters.md §4.5).
//
// Jev is text-only and reads English structured data best, so only the structured fields of the
// Judge's Issues (type, severity, dimension, time range) are sent: no free-form description, no
// Brief, script, image or video. The version is pinned (never an alias such as jev-latest) and the
// API key is read from credential.Store on every call.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
)

// CredentialProvider is the credential.Store key of the TypeSafe API key.
const CredentialProvider = "typesafe"

// ErrNoAPIKey means no TypeSafe API key is stored: the shadow run is skipped.
var ErrNoAPIKey = errors.New("jev: no TypeSafe API key is configured")

// Defaults. The model is a fixed version: an alias would change the answers when the vendor updates it.
const (
	DefaultBaseURL = "https://api.typesafe.ai"
	DefaultModel   = "jev-1.13.0"
	// DefaultInputCostPerMillionUSD is the published price of one million input tokens (output is free).
	DefaultInputCostPerMillionUSD = 0.042
	defaultTimeout                = 10 * time.Second
	maxResponseBytes              = 1 << 20
)

var versionRe = regexp.MustCompile(`^jev-\d+\.\d+\.\d+$`)

// Config is the Jev setting.
type Config struct {
	// BaseURL is the API origin.
	BaseURL string `json:"base_url"`
	// Model is the pinned Jev version, "jev-<major>.<minor>.<patch>".
	Model string `json:"model"`
	// TimeoutSeconds bounds one call.
	TimeoutSeconds int `json:"timeout_seconds"`
	// InputCostPerMillionUSD prices the input tokens of a call (USD per million).
	InputCostPerMillionUSD float64 `json:"input_cost_per_million_usd"`
}

// DefaultConfig is the setting used when nothing overrides it.
func DefaultConfig() Config {
	return Config{BaseURL: DefaultBaseURL, Model: DefaultModel, TimeoutSeconds: int(defaultTimeout / time.Second), InputCostPerMillionUSD: DefaultInputCostPerMillionUSD}
}

// Validate rejects an unpinned model ("jev-latest", "jev-preview") and unusable values.
func (c Config) Validate() error {
	switch {
	case !versionRe.MatchString(c.Model):
		return fmt.Errorf("jev: model %q is not a pinned version (want jev-<major>.<minor>.<patch>; aliases such as jev-latest are not allowed)", c.Model)
	case c.BaseURL == "":
		return errors.New("jev: base_url is empty")
	case c.TimeoutSeconds <= 0:
		return errors.New("jev: timeout_seconds must be positive")
	case c.InputCostPerMillionUSD < 0:
		return errors.New("jev: input_cost_per_million_usd must not be negative")
	}
	return nil
}

// LoadConfig returns DefaultConfig overridden by the JSON file at path ("" selects the default).
// Unknown keys are an error.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the operator chooses the settings file
	if err != nil {
		return Config{}, fmt.Errorf("read jev config: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode jev config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("jev config %s: %w", path, err)
	}
	return cfg, nil
}

// Issue is what is sent of a Judge Issue: schema values only.
type Issue struct {
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	Dimension string `json:"dimension,omitempty"`
	TimeRange string `json:"time_range,omitempty"`
}

// Input is the question put to Jev.
type Input struct {
	Issues []domain.JudgeIssue
	// Candidates are the repair strategies Jev may choose from.
	Candidates []domain.RepairAction
}

// Choice is Jev's answer.
type Choice struct {
	Action        domain.RepairAction
	Confidence    float64
	Probabilities map[string]float64
	// Model is the version that answered.
	Model        string
	InputTokens  int
	OutputTokens int
	Latency      time.Duration
	CostUSD      float64
}

// rubric describes the strategies to Jev, in English.
var rubric = map[domain.RepairAction]string{
	domain.RepairPrompt:              "Revise the text prompt so that the described flaws are avoided",
	domain.RepairPartialRegeneration: "Regenerate the shot while keeping the parts that were good unchanged",
	domain.RepairFullRetake:          "Generate the shot again from the same prompt",
	domain.RepairDifferentModel:      "Generate the shot again with a different video model",
	domain.RepairReferenceReinforce:  "Generate the shot again with stronger reference images of the character or product",
	domain.RepairEditLevel:           "Keep the take and fix the flaws in editing (trim or cut away)",
}

// Client calls the Jev API. It is safe for concurrent use.
type Client struct {
	cfg  Config
	keys credential.Store
	http *http.Client
	now  func() time.Time
}

// New returns a Client. httpClient may be nil.
func New(cfg Config, keys credential.Store, httpClient *http.Client) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = httpclient.New()
	}
	return &Client{cfg: cfg, keys: keys, http: httpClient, now: time.Now}, nil
}

// Model is the pinned version the Client asks.
func (c *Client) Model() string { return c.cfg.Model }

type request struct {
	State     state               `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]question `json:"questions"`
}

type state struct {
	Issues []Issue `json:"issues"`
}

type question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type response struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Type          string             `json:"type"`
		Choice        string             `json:"choice"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    float64            `json:"confidence"`
	} `json:"answers"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

const questionID = "repair_strategy"

// Choose asks Jev which of the candidate strategies fits the Issues.
func (c *Client) Choose(ctx context.Context, in Input) (Choice, error) {
	if len(in.Candidates) == 0 {
		return Choice{}, errors.New("jev: no candidate strategies")
	}
	key, err := c.keys.Get(CredentialProvider)
	if errors.Is(err, credential.ErrNotFound) {
		return Choice{}, ErrNoAPIKey
	}
	if err != nil {
		return Choice{}, err
	}

	st := state{Issues: make([]Issue, 0, len(in.Issues))}
	for _, is := range in.Issues {
		st.Issues = append(st.Issues, Issue{Type: is.Type, Severity: string(is.Severity), Dimension: string(is.Dimension), TimeRange: is.TimeRange})
	}
	criteria := make(map[string]string, len(in.Candidates))
	for _, a := range in.Candidates {
		criteria[string(a)] = rubric[a]
	}
	body, err := json.Marshal(request{State: st, Model: c.cfg.Model, Questions: map[string]question{
		questionID: {Type: "choice", Instructions: "Which repair strategy fits the issues found in this AI-generated video take?", Criteria: criteria},
	}})
	if err != nil {
		return Choice{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return Choice{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	start := c.now()
	res, err := c.http.Do(req)
	if err != nil {
		return Choice{}, fmt.Errorf("jev: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	latency := c.now().Sub(start)
	if err != nil {
		return Choice{}, fmt.Errorf("jev: read response: %w", err)
	}
	if res.StatusCode/100 != 2 {
		return Choice{}, fmt.Errorf("jev: status %d", res.StatusCode)
	}
	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return Choice{}, fmt.Errorf("jev: decode response: %w", err)
	}
	ans, ok := out.Answers[questionID]
	if !ok || ans.Type != "choice" || ans.Choice == "" {
		return Choice{}, errors.New("jev: the response has no choice")
	}
	return Choice{
		Action: domain.RepairAction(ans.Choice), Confidence: ans.Confidence, Probabilities: ans.Probabilities, Model: out.Model,
		InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens, Latency: latency,
		CostUSD: float64(out.Usage.InputTokens) * c.cfg.InputCostPerMillionUSD / 1e6,
	}, nil
}
