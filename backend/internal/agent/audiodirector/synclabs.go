package audiodirector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// SyncCredentialProvider is the credential.Store key of the Sync Labs API key (model identifier
// sync-labs of spec/architecture/model-adapters.md §6).
const SyncCredentialProvider = "synclabs"

// ErrNoSyncLabsKey means no Sync Labs API key is stored.
var ErrNoSyncLabsKey = errors.New("audiodirector: Sync Labs API key is not configured")

// LipSyncRequest is one Lip Sync: the video whose mouth is redone and the speech it must follow.
type LipSyncRequest struct {
	// Model is the Sync Labs model ("lipsync-2", "sync-3", ...).
	Model string
	Video Sample
	Audio Sample
}

// LipSyncer redoes the mouth of a video to a speech and returns the new video (mp4).
// *SyncLabs implements it.
type LipSyncer interface {
	Sync(ctx context.Context, req LipSyncRequest) ([]byte, error)
}

// SyncLabsConfig is the Sync Labs setting.
type SyncLabsConfig struct {
	BaseURL string
	// Timeout bounds one Lip Sync from upload to download.
	Timeout time.Duration
	// PollInterval is the pause between two status requests.
	PollInterval time.Duration
	// LocalDownloads lets the output be downloaded from a plain http URL or a local address. It
	// exists for tests against httptest servers; by default the output URL must be https and
	// the download only connects to public addresses.
	LocalDownloads bool
}

// DefaultSyncLabsConfig is the setting used unless the caller changes it.
func DefaultSyncLabsConfig() SyncLabsConfig {
	return SyncLabsConfig{BaseURL: "https://api.sync.so", Timeout: 10 * time.Minute, PollInterval: 2 * time.Second}
}

// maxLipSyncBytes bounds a lip-synced video.
const maxLipSyncBytes = 512 << 20

// SyncLabs is the Sync Labs Lip Sync adapter (POST /v2/generate, then poll GET /v2/generate/{id}).
type SyncLabs struct {
	cfg    SyncLabsConfig
	keys   credential.Store
	client Doer
	// fetcher downloads the output, whose URL comes from the vendor's answer.
	fetcher Doer
}

// NewSyncLabs returns a SyncLabs adapter. A nil client selects a plain http.Client for the API and
// a client that connects to public addresses only (safefetch.NewClient) for the download; a given
// client serves both. Zero fields of cfg take the DefaultSyncLabsConfig values.
func NewSyncLabs(cfg SyncLabsConfig, keys credential.Store, client Doer) *SyncLabs {
	def := DefaultSyncLabsConfig()
	if cfg.BaseURL == "" {
		cfg.BaseURL = def.BaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = def.PollInterval
	}
	fetcher := client
	if client == nil {
		client = &http.Client{}
		fetcher = client
		if !cfg.LocalDownloads {
			fetcher = safefetch.NewClient()
		}
	}
	return &SyncLabs{cfg: cfg, keys: keys, client: client, fetcher: fetcher}
}

// generation is the part of the Sync Labs generation object the adapter reads.
type generation struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	OutputURL string `json:"outputUrl"`
}

// Sync implements LipSyncer.
func (s *SyncLabs) Sync(ctx context.Context, req LipSyncRequest) ([]byte, error) {
	key, err := s.keys.Get(SyncCredentialProvider)
	if errors.Is(err, credential.ErrNotFound) {
		return nil, ErrNoSyncLabsKey
	}
	if err != nil {
		return nil, fmt.Errorf("read Sync Labs API key: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	fail := func(err error) error {
		var pe *ProviderError
		if errors.As(err, &pe) || errors.Is(err, context.Canceled) {
			return err
		}
		return &ProviderError{Vendor: VendorSyncLabs, Err: redact(err, key)}
	}

	gen, err := s.create(ctx, key, req)
	if err != nil {
		return nil, fail(err)
	}
	for gen.Status != "COMPLETED" {
		switch gen.Status {
		case "FAILED", "REJECTED":
			return nil, &ProviderError{Vendor: VendorSyncLabs, Message: gen.Status + ": " + gen.Error}
		}
		select {
		case <-ctx.Done():
			return nil, fail(ctx.Err())
		case <-time.After(s.cfg.PollInterval):
		}
		if gen, err = s.status(ctx, key, gen.ID); err != nil {
			return nil, fail(err)
		}
	}
	if gen.OutputURL == "" {
		return nil, &ProviderError{Vendor: VendorSyncLabs, Message: "the generation has no outputUrl"}
	}
	video, err := s.download(ctx, gen.OutputURL)
	if err != nil {
		return nil, fail(err)
	}
	return video, nil
}

func (s *SyncLabs) create(ctx context.Context, key string, req LipSyncRequest) (generation, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("model", req.Model); err != nil {
		return generation{}, err
	}
	for _, f := range []struct {
		field string
		s     Sample
	}{{"video", req.Video}, {"audio", req.Audio}} {
		part, err := mw.CreateFormFile(f.field, f.s.Filename)
		if err != nil {
			return generation{}, err
		}
		if _, err := part.Write(f.s.Data); err != nil {
			return generation{}, err
		}
	}
	if err := mw.Close(); err != nil {
		return generation{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url("/v2/generate"), &buf)
	if err != nil {
		return generation{}, err
	}
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return s.send(r, key)
}

func (s *SyncLabs) status(ctx context.Context, key, id string) (generation, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url("/v2/generate/"+url.PathEscape(id))+"?wait=true&timeout=10", nil)
	if err != nil {
		return generation{}, err
	}
	return s.send(r, key)
}

func (s *SyncLabs) url(path string) string { return strings.TrimRight(s.cfg.BaseURL, "/") + path }

// send runs a request of the API and reads the generation it answers with.
func (s *SyncLabs) send(r *http.Request, key string) (generation, error) {
	r.Header.Set("x-api-key", key)
	r.Header.Set("Accept", "application/json")
	res, err := s.client.Do(r)
	if err != nil {
		return generation{}, err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return generation{}, err
	}
	if res.StatusCode/100 != 2 {
		return generation{}, &ProviderError{Vendor: VendorSyncLabs, Status: res.StatusCode, Message: snippet(data)}
	}
	var g generation
	if err := json.Unmarshal(data, &g); err != nil || g.ID == "" {
		return generation{}, &ProviderError{Vendor: VendorSyncLabs, Status: res.StatusCode, Message: "the answer is not a generation"}
	}
	return g, nil
}

// download fetches the output video. The API key is not sent: the URL is a signed link of the vendor's storage.
func (s *SyncLabs) download(ctx context.Context, link string) ([]byte, error) {
	if err := s.checkOutputURL(link); err != nil {
		return nil, &ProviderError{Vendor: VendorSyncLabs, Message: err.Error()}
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.fetcher.Do(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return nil, &ProviderError{Vendor: VendorSyncLabs, Status: res.StatusCode, Message: "download of the output failed"}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxLipSyncBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxLipSyncBytes {
		return nil, &ProviderError{Vendor: VendorSyncLabs, Status: res.StatusCode, Message: "the output is empty or too large"}
	}
	return data, nil
}

// checkOutputURL refuses an output URL that is not an https URL of a public host.
func (s *SyncLabs) checkOutputURL(link string) error {
	if s.cfg.LocalDownloads {
		if u, err := url.Parse(link); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return errors.New("the output URL is not an http(s) URL")
		}
		return nil
	}
	if err := safefetch.CheckURL(link); err != nil {
		return errors.New(strings.Replace(err.Error(), "result URL", "output URL", 1))
	}
	return nil
}
