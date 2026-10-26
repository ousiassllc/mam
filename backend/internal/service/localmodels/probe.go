package localmodels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/safefetch"
)

// probeTimeout bounds a connection test.
const probeTimeout = 10 * time.Second

// minSafeCancel is the oldest ComfyUI whose POST /interrupt can be scoped to one prompt (0.3.57); on an older
// server a cancelled call could stop the jobs of other clients, so the test says so.
var minSafeCancel = [3]int{0, 3, 57}

// TestInput is an endpoint to test, saved or not. With Name only, the saved endpoint is tested with its saved
// key. With Kind and BaseURL the draft is tested; APIKey is its key (empty: the saved key of Name, if any).
type TestInput struct {
	Name    string
	Kind    localcfg.Kind
	BaseURL string
	APIKey  string
}

// TestResult is the outcome of a connection test. OK is false when the endpoint did not answer as that kind
// of server should; Message says what happened. Note is a caveat that does not make it a failure ("/v1/models
// is not provided", a ComfyUI that is too old to cancel safely). Version is the ComfyUI version and
// Models the model identifiers an OpenAI-compatible server lists (at most 20).
type TestResult struct {
	OK      bool
	Message string
	Note    string
	Version string
	Models  []string
}

// TestEndpoint tests a saved endpoint (ComfyUI: GET /system_stats; OpenAI-compatible: GET /v1/models).
func (s *Service) TestEndpoint(ctx context.Context, name string) (TestResult, error) {
	return s.Test(ctx, TestInput{Name: name})
}

// Test sends the connection test. A transport failure or an unexpected answer is a TestResult with OK false,
// not an error; an unknown name or an unusable draft is ErrNotFound / *InvalidError.
func (s *Service) Test(ctx context.Context, in TestInput) (TestResult, error) {
	cfg, err := s.load()
	if err != nil {
		return TestResult{}, err
	}
	name, e := in.Name, localcfg.Endpoint{Kind: in.Kind, BaseURL: strings.TrimSpace(in.BaseURL)}
	if in.Kind == "" && in.BaseURL == "" {
		saved, ok := cfg.Endpoints[name]
		if !ok {
			return TestResult{}, ErrNotFound
		}
		e = saved
	} else if err := endpointError(localcfg.ValidateEndpoint(cmpName(name), e)); err != nil {
		return TestResult{}, err
	}
	keys := s.Keys
	if in.APIKey != "" {
		keys = draftKey{vendor: localcfg.VendorKey(cmpName(name)), key: strings.TrimSpace(in.APIKey)}
	}
	c, err := localhttp.New(cmpName(name), e, keys, "")
	if err != nil {
		return TestResult{}, invalid("%v", err)
	}
	if s.Probe != nil {
		c.Requests = s.Probe
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if e.Kind == localcfg.KindComfyUI {
		return probeComfyUI(ctx, c), nil
	}
	return probeCompat(ctx, c), nil
}

func cmpName(name string) string {
	if name == "" {
		return "draft"
	}
	return name
}

// draftKey is the Store of a test with a key that is not saved: it answers for one vendor only.
type draftKey struct{ vendor, key string }

func (d draftKey) Set(string, string) error { return errors.New("read only") }
func (d draftKey) Delete(string) error      { return errors.New("read only") }
func (d draftKey) Get(p string) (string, error) {
	if p == d.vendor {
		return d.key, nil
	}
	return "", credential.ErrNotFound
}
func (d draftKey) Has(p string) (bool, error) { return p == d.vendor, nil }

func probeComfyUI(ctx context.Context, c *localhttp.Client) TestResult {
	data, err := c.Request(ctx, http.MethodGet, "/system_stats", nil, nil, 1<<20)
	if res, failed := failure(err, "ComfyUI"); failed {
		return res
	}
	var stats struct {
		System struct {
			Version string `json:"comfyui_version"`
		} `json:"system"`
		Devices []struct {
			Name string `json:"name"`
		} `json:"devices"`
	}
	if json.Unmarshal(data, &stats) != nil || (stats.System.Version == "" && len(stats.Devices) == 0) {
		return TestResult{Message: "接続できましたが、ComfyUI の応答ではありません（/system_stats の形が違います）。種類や URL を確かめてください"}
	}
	res := TestResult{OK: true, Version: stats.System.Version, Message: "ComfyUI に接続できました"}
	if stats.System.Version != "" {
		res.Message += "（バージョン " + stats.System.Version + "）"
	}
	if len(stats.Devices) > 0 && stats.Devices[0].Name != "" {
		res.Message += "。デバイス: " + stats.Devices[0].Name
	}
	if v, ok := parseVersion(stats.System.Version); ok && less(v, minSafeCancel) {
		res.Note = "このバージョンの ComfyUI は中断を 1 つのジョブに限れません。mam から中断すると他の人のジョブも止まる恐れがあります（0.3.57 以降を推奨）"
	}
	return res
}

func probeCompat(ctx context.Context, c *localhttp.Client) TestResult {
	data, err := c.Request(ctx, http.MethodGet, "/v1/models", nil, nil, 4<<20)
	var pe *modeladapter.ProviderError
	if errors.As(err, &pe) && (pe.Status == http.StatusNotFound || pe.Status == http.StatusMethodNotAllowed) {
		return TestResult{OK: true, Message: "サーバーに到達できました", Note: "/v1/models は提供されていません（画像の API は使える場合があります）"}
	}
	if res, failed := failure(err, "OpenAI 互換サーバー"); failed {
		return res
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &list) != nil {
		return TestResult{Message: "接続できましたが、OpenAI 互換サーバーの応答ではありません（/v1/models の形が違います）"}
	}
	res := TestResult{OK: true, Message: fmt.Sprintf("OpenAI 互換サーバーに接続できました（モデル %d 件）", len(list.Data))}
	for _, m := range list.Data[:min(len(list.Data), 20)] {
		res.Models = append(res.Models, m.ID)
	}
	return res
}

// failure turns a failed request into a TestResult that says why in words the user can act on.
func failure(err error, what string) (TestResult, bool) {
	if err == nil {
		return TestResult{}, false
	}
	var pe *modeladapter.ProviderError
	switch {
	case errors.Is(err, safefetch.ErrRefused):
		return TestResult{Message: "このアドレスには接続できません（リンクローカル・未指定・マルチキャストのアドレスは使えません）"}, true
	case errors.As(err, &pe) && (pe.Status == http.StatusUnauthorized || pe.Status == http.StatusForbidden):
		return TestResult{Message: "認証に失敗しました（HTTP " + strconv.Itoa(pe.Status) + "）。API キーを確かめてください"}, true
	case errors.As(err, &pe) && pe.Status != 0:
		return TestResult{Message: fmt.Sprintf("%s が HTTP %d を返しました。種類や URL を確かめてください", what, pe.Status)}, true
	case errors.Is(err, context.DeadlineExceeded):
		return TestResult{Message: "接続がタイムアウトしました（" + probeTimeout.String() + "）。サーバーが起動しているか、アドレスとポートを確かめてください"}, true
	}
	return TestResult{Message: "接続できませんでした: " + err.Error()}, true
}

// parseVersion reads "0.3.57" (a leading "v" and a suffix after "-" or "+" are ignored).
func parseVersion(v string) ([3]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "-")
	v, _, _ = strings.Cut(v, "+")
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, v != ""
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
