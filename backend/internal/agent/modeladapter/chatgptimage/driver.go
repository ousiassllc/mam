package chatgptimage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/chatgpt"
)

// The Codex model that hosts the image_generation tool call (the image model itself travels inside the tool) is not
// fixed: OpenAI replaces the models a ChatGPT sign-in may use (GPT-5.5 retired for it on 2026-10-14, and the endpoint then
// answers 400 "The 'gpt-5.5' model is not supported when using Codex with a ChatGPT account"). The backend therefore picks
// the driver by itself:
//
//  1. Options.Driver, when set, is used as it is (an explicit override, no fallback).
//  2. Otherwise the driver that worked last is used again (remembered for the process).
//  3. Otherwise, or when the remembered driver is refused (it stopped working), the models of the account are read from
//     the Codex model list (GET {base}/models?client_version=..., what the Codex CLI reads: models-manager of
//     github.com/openai/codex) and tried in the list's own order (priority), skipping the ones the picker hides.
//  4. When the list cannot be read or is empty, FallbackDrivers are tried instead, in order.
//
// A driver is given up for the next one only when the endpoint says the model is not usable (driverRefused); any other
// failure (a moderation stop, rate limit, network, ...) ends the call as it is.

// FallbackDrivers are the Codex models tried when the model list of the account is unavailable. They are the replacements
// of the retired GPT-5.5 named by https://developers.openai.com/codex/models (checked 2026-11-01): gpt-6.1-sol for Plus,
// Pro, Business, Enterprise and Edu, gpt-6-luna for Free and Go, gpt-6.1-terra (Codex 0.159.0 or later), gpt-6-sol.
var FallbackDrivers = []string{"gpt-6.1-sol", "gpt-6-luna", "gpt-6.1-terra", "gpt-6-sol"}

const (
	// listTimeout bounds the read of the model list.
	listTimeout = 20 * time.Second
	// maxListBytes bounds the model list (the Codex list of today is about 150 KB).
	maxListBytes = 4 << 20
	// visibilityList is the visibility of a model the Codex picker shows.
	visibilityList = "list"
)

// driverLoop runs call with the driver to use, moving on to the next one while the endpoint refuses the model.
func (b *Backend) driverLoop(ctx context.Context, creds chatgpt.Credentials, call func(driver string) (modeladapter.ImageResult, error)) (modeladapter.ImageResult, error) {
	var tried []string
	var last error
	if d := b.firstDriver(); d != "" {
		res, err := call(d)
		if !driverRefused(err, d) {
			b.note(d, err)
			return res, err
		}
		b.forget(d)
		tried, last = append(tried, d), err
	}
	if b.pinned == "" {
		for _, d := range b.discover(ctx, creds) {
			if slices.Contains(tried, d) {
				continue
			}
			res, err := call(d)
			if !driverRefused(err, d) {
				b.note(d, err)
				return res, err
			}
			tried, last = append(tried, d), err
		}
	}
	return modeladapter.ImageResult{}, noDriver(tried, last)
}

// firstDriver is the driver to try first: the pinned one, else the remembered one ("" when there is none).
func (b *Backend) firstDriver() string {
	if b.pinned != "" {
		return b.pinned
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.picked
}

// note remembers a driver that worked.
func (b *Backend) note(driver string, err error) {
	if err != nil || b.pinned != "" {
		return
	}
	b.mu.Lock()
	b.picked = driver
	b.mu.Unlock()
}

// forget drops the remembered driver when it is still driver.
func (b *Backend) forget(driver string) {
	b.mu.Lock()
	if b.picked == driver {
		b.picked = ""
	}
	b.mu.Unlock()
}

// discover is the drivers to try, best first: the models of the account, else FallbackDrivers.
func (b *Backend) discover(ctx context.Context, creds chatgpt.Credentials) []string {
	if models := b.listModels(ctx, creds); len(models) > 0 {
		return models
	}
	return FallbackDrivers
}

// listModels reads the Codex model list of the account: the slugs of the models the picker shows, by priority. A list that
// cannot be read is no failure of the call (the fallback drivers are tried), so it answers nil.
func (b *Backend) listModels(ctx context.Context, creds chatgpt.Credentials) []string {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	r, err := b.newRequest(ctx, http.MethodGet, b.baseURL+"/models?client_version="+url.QueryEscape(b.clientVersion), nil, creds)
	if err != nil {
		return nil
	}
	r.Header.Set("Accept", "application/json")
	res, err := b.client.Do(r)
	if err != nil {
		return nil
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxListBytes))
	if err != nil {
		return nil
	}
	return pickable(data)
}

// pickable is the slugs of the models of a Codex model list (the shape of models_cache.json: {"models": [{slug,
// visibility, priority, ...}]}) that the picker shows, lowest priority number first.
func pickable(data []byte) []string {
	var list struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
			Priority   int    `json:"priority"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	shown := list.Models[:0]
	for _, m := range list.Models {
		if m.Slug != "" && m.Visibility == visibilityList {
			shown = append(shown, m)
		}
	}
	sort.SliceStable(shown, func(i, j int) bool { return shown[i].Priority < shown[j].Priority })
	var slugs []string
	for _, m := range shown {
		if !slices.Contains(slugs, m.Slug) {
			slugs = append(slugs, m.Slug)
		}
	}
	return slugs
}

// driverRefused reports whether err says the endpoint does not accept driver as the model of the call, for example
// 400 "The 'gpt-5.5' model is not supported when using Codex with a ChatGPT account". Only that moves the loop on.
func driverRefused(err error, driver string) bool {
	var pe *modeladapter.ProviderError
	if !errors.As(err, &pe) || (pe.Status != http.StatusBadRequest && pe.Status != http.StatusNotFound) {
		return false
	}
	m := strings.ToLower(pe.Message)
	if !strings.Contains(m, "model") {
		return false
	}
	if !strings.Contains(m, "not supported") && !strings.Contains(m, "not found") && !strings.Contains(m, "does not exist") {
		return false
	}
	return strings.Contains(m, strings.ToLower(driver)) || strings.Contains(m, "chatgpt account")
}

// noDriver is the failure when the endpoint refused every driver. It is a 400 (a refusal of the request, which no retry
// helps) that is chatgpt.ErrNoImageModel, so that the screens explain it in words instead of showing the vendor's text.
func noDriver(tried []string, last error) error {
	cause := ""
	var pe *modeladapter.ProviderError
	if errors.As(last, &pe) {
		cause = ": " + pe.Message
	}
	return &modeladapter.ProviderError{
		Vendor: vendor, Status: http.StatusBadRequest, Err: chatgpt.ErrNoImageModel,
		Message: fmt.Sprintf("no Codex model that the ChatGPT account may use hosts the image tool call (tried %s)%s", strings.Join(tried, ", "), cause),
	}
}
