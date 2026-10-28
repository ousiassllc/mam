package modeladapter

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// klingBackend calls Kling 3.0. Its API key is "ACCESS_KEY:SECRET_KEY" (as issued by Kling); every
// request carries a short-lived HS256 token made from it.
type klingBackend struct {
	api
	now func() time.Time
}

func (b klingBackend) token(key string) (string, error) {
	access, secret, ok := strings.Cut(key, ":")
	if !ok || access == "" || secret == "" {
		return "", fmt.Errorf("%w: the kling key must be ACCESS_KEY:SECRET_KEY", ErrNoAPIKey)
	}
	now := b.now()
	enc := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	unsigned := enc(map[string]string{"alg": "HS256", "typ": "JWT"}) + "." +
		enc(map[string]any{"iss": access, "exp": now.Add(30 * time.Minute).Unix(), "nbf": now.Add(-5 * time.Second).Unix()})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// klingEnvelope is the answer shape of every Kling endpoint: code 0 is success.
type klingEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		TaskID     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
		StatusMsg  string `json:"task_status_msg"`
		Result     struct {
			Videos []struct {
				URL string `json:"url"`
			} `json:"videos"`
		} `json:"task_result"`
	} `json:"data"`
}

// check turns a non-zero Kling code into an error.
func (b klingBackend) check(env klingEnvelope) error {
	return klingCodeError(b.vendor, env.Code, env.Message)
}

func (b klingBackend) GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error) {
	key, err := b.key()
	if err != nil {
		return VideoResult{}, err
	}
	token, err := b.token(key)
	if err != nil {
		return VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + token}

	mode := "std"
	if req.Resolution == "1080p" {
		mode = "pro"
	}
	body := map[string]any{
		"model_name": apiModel, "prompt": req.Prompt, "negative_prompt": req.NegativePrompt,
		"duration": strconv.Itoa(int(req.DurationSeconds)), "aspect_ratio": req.AspectRatio, "mode": mode,
	}
	path := "/v1/videos/text2video"
	if req.FirstFrame != nil {
		path = "/v1/videos/image2video"
		body["image"] = base64.StdEncoding.EncodeToString(req.FirstFrame.Data)
		if req.LastFrame != nil {
			body["image_tail"] = base64.StdEncoding.EncodeToString(req.LastFrame.Data)
		}
	}
	raw, _ := json.Marshal(body)
	data, err := b.request(ctx, http.MethodPost, b.baseURL+path, jsonHeader(auth), raw, maxJSONBytes)
	if err != nil {
		return VideoResult{}, err
	}
	var submit klingEnvelope
	if err := unmarshal(b.api, data, &submit); err != nil {
		return VideoResult{}, err
	}
	if err := b.check(submit); err != nil {
		return VideoResult{}, err
	}
	if submit.Data.TaskID == "" {
		return VideoResult{}, b.empty("task id")
	}

	var videoURL string
	err = b.poll(ctx, func(ctx context.Context) (bool, error) {
		d, err := b.request(ctx, http.MethodGet, b.baseURL+path+"/"+url.PathEscape(submit.Data.TaskID), auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st klingEnvelope
		if err := unmarshal(b.api, d, &st); err != nil {
			return false, err
		}
		if err := b.check(st); err != nil {
			return false, err
		}
		switch st.Data.TaskStatus {
		case "succeed":
			if len(st.Data.Result.Videos) == 0 {
				return false, b.empty("video")
			}
			videoURL = st.Data.Result.Videos[0].URL
			return true, nil
		case "failed":
			return false, b.failed("the video job failed: " + st.Data.StatusMsg)
		}
		return false, nil
	})
	if err != nil {
		return VideoResult{}, err
	}
	file, mime, err := b.download(ctx, videoURL)
	if err != nil {
		return VideoResult{}, err
	}
	return VideoResult{File: file, MimeType: videoMime(mime)}, nil
}

func videoMime(header string) string {
	m := strings.TrimSpace(strings.SplitN(header, ";", 2)[0])
	if strings.HasPrefix(m, "video/") {
		return m
	}
	return "video/mp4"
}

// BackendOption changes how NewBackends builds the backends.
type BackendOption func(*backendOptions)

type backendOptions struct {
	localDownloads bool
	stagingDir     string
}

// WithLocalDownloads lets result files be downloaded from plain http URLs and local addresses, and
// through the client given to NewBackends. It exists for tests against httptest servers: without
// it a result URL must be an https URL of a public host.
func WithLocalDownloads() BackendOption { return func(o *backendOptions) { o.localDownloads = true } }

// WithStagingDir sets where downloaded videos are staged until the caller stores them (see Blob).
// It should be on the volume the files are stored on, so that storing is a rename.
func WithStagingDir(dir string) BackendOption { return func(o *backendOptions) { o.stagingDir = dir } }

// NewBackends returns the HTTP backend of every model. API keys are read from keys at call time,
// so a key stored in the settings screen takes effect without a restart. client may be nil (a
// client without an overall timeout: the Adapter bounds every call). pollEvery is the wait
// between two status requests of an asynchronous job (zero selects 5 seconds). Result files are
// downloaded with a client that connects to public addresses only (safefetch.NewClient) when
// client is nil, otherwise with client; either way the URL must be https (see WithLocalDownloads).
func NewBackends(cfg Config, keys credential.Store, client httpclient.Doer, pollEvery time.Duration, opts ...BackendOption) Backends {
	var o backendOptions
	for _, opt := range opts {
		opt(&o)
	}
	fetcher := client
	if client == nil {
		client = httpclient.New()
		fetcher = client
		if !o.localDownloads {
			fetcher = safefetch.NewClient()
		}
	}
	if pollEvery <= 0 {
		pollEvery = 5 * time.Second
	}
	mk := func(v Vendor) api {
		return api{
			vendor: v, baseURL: strings.TrimRight(cfg.Vendors[v].BaseURL, "/"), keys: keys,
			client: client, fetcher: fetcher, localDownloads: o.localDownloads, pollEvery: pollEvery, stagingDir: o.stagingDir,
		}
	}
	return Backends{
		Image: map[domain.ModelName]ImageBackend{
			domain.ModelNanoBanana:     geminiImageBackend{mk(VendorGoogle)},
			domain.ModelNanoBananaLite: geminiImageBackend{mk(VendorGoogle)},
			domain.ModelGPTImage2:      openAIImageBackend{mk(VendorOpenAI), gptimage.Effective(cfg.Models[domain.ModelGPTImage2].Quality)},
			domain.ModelFlux2:          fluxBackend{mk(VendorFlux)},
		},
		Video: map[domain.ModelName]VideoBackend{
			domain.ModelKling3:          klingBackend{api: mk(VendorKling), now: time.Now},
			domain.ModelSeedance2:       seedanceBackend{api: mk(VendorSeedance)},
			domain.ModelSeedance25:      seedanceBackend{api: mk(VendorSeedance), fields: true},
			domain.ModelRunwayGen45:     runwayBackend{mk(VendorRunway)},
			domain.ModelGeminiOmniFlash: geminiOmniBackend{mk(VendorGoogle)},
			domain.ModelKling4:          kling4Backend{mk(VendorKling4)},
			domain.ModelMiniMaxH3:       minimaxBackend{mk(VendorMiniMax)},
			domain.ModelMiniMaxH3Max:    minimaxBackend{mk(VendorMiniMax)},
			domain.ModelWan3:            alibabaBackend{mk(VendorAlibaba)},
		},
	}
}
