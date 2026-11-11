// Package providerbackends calls the cloud video models through an API aggregator (OpenRouter, fal.ai, Replicate): the
// VideoBackends that stand in for the maker's own ones while the provider setting (modeladapter.Config.VideoProvider) selects
// one (spec/architecture/model-adapters.md §9.6). Which endpoint serves which model, at what price, is package viaprovider.
// Every backend submits the job, polls it and downloads the video, like the official ones; the API key is the aggregator's
// (credential.Store key viaprovider.Provider.Vendor) and goes in the request header only.
package providerbackends

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Install makes video use the aggregator of cfg.VideoProvider for every model of viaprovider.Models it serves, and a backend that
// refuses, with the reason, for a model it does not serve: a call must never fall back to the maker's own account unasked (it would
// be billed there). mk is modeladapter.APIs. Nothing changes for the official provider.
func Install(video map[domain.ModelName]modeladapter.VideoBackend, cfg modeladapter.Config, mk func(modeladapter.Vendor) modeladapter.API) {
	for _, r := range viaprovider.Routes(cfg.VideoProvider) {
		if !r.Supported() {
			video[r.Model] = refusal{vendor: r.Provider.Vendor(), why: r.Why}
			continue
		}
		a := mk(modeladapter.Vendor(r.Provider.Vendor()))
		switch r.Provider {
		case viaprovider.OpenRouter:
			video[r.Model] = openRouter{API: a, route: r}
		case viaprovider.Fal:
			video[r.Model] = fal{API: a, route: r}
		case viaprovider.Replicate:
			video[r.Model] = replicate{API: a, route: r}
		}
	}
}

// refusal is the backend of a model the selected provider does not serve.
type refusal struct{ vendor, why string }

func (r refusal) GenerateVideo(context.Context, string, modeladapter.VideoRequest) (modeladapter.VideoResult, error) {
	return modeladapter.VideoResult{}, &modeladapter.ProviderError{Vendor: r.vendor, Status: http.StatusUnprocessableEntity, Message: r.why}
}

// variant is how the provider serves the resolution of the request; a resolution it does not have is a request that cannot be served
// (the Adapter routes to the alternative).
func variant(route viaprovider.Route, req modeladapter.VideoRequest) (viaprovider.Variant, error) {
	v, ok := route.Variants[req.Resolution]
	if !ok {
		return v, &modeladapter.ProviderError{Vendor: route.Provider.Vendor(), Status: http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("%s has no %s variant of %s", route.Provider.Label(), req.Resolution, viaprovider.ModelLabel(route.Model))}
	}
	return v, nil
}

func dataURI(in modeladapter.Input) string {
	return "data:" + in.MimeType + ";base64," + base64.StdEncoding.EncodeToString(in.Data)
}

// submit posts body to target and decodes the answer into out.
func submit(ctx context.Context, a modeladapter.API, target string, header map[string]string, body map[string]any, out any) error {
	raw, _ := json.Marshal(body)
	data, err := a.Request(ctx, http.MethodPost, target, modeladapter.JSONHeader(header), raw)
	if err != nil {
		return err
	}
	return a.Decode(data, out)
}

// fetch gets target and decodes the answer into out.
func fetch(ctx context.Context, a modeladapter.API, target string, header map[string]string, out any) error {
	data, err := a.Request(ctx, http.MethodGet, target, header, nil)
	if err != nil {
		return err
	}
	return a.Decode(data, out)
}

// finish downloads the video at fileURL (auth: the headers the URL needs, for the aggregator's own host).
func finish(ctx context.Context, a modeladapter.API, fileURL string, auth map[string]string) (modeladapter.VideoResult, error) {
	file, mime, err := a.Download(ctx, fileURL, auth)
	if err != nil {
		return modeladapter.VideoResult{}, err
	}
	return modeladapter.VideoResult{File: file, MimeType: modeladapter.VideoMime(mime)}, nil
}

// failedJob is the error of a job that ended without a video; the reason is the aggregator's own words, trimmed.
func failedJob(a modeladapter.API, status, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "status " + status
	}
	return a.Failed("the video job failed: " + reason)
}
