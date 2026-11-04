// Package extfolder reads the model folders of an external ComfyUI server (GET /models/<folder>). It is split off package modelmanager, which is at its line limit.
package extfolder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

// Folders are the ComfyUI model folders the catalog installs into.
var Folders = []string{"checkpoints", "diffusion_models", "text_encoders", "vae", "loras", "latent_upscale_models", "controlnet", "clip_vision", "ipadapter", "upscale_models"}

// List is GET /models/<folder>; a folder the server does not have (404) is empty.
func List(ctx context.Context, c *localhttp.Client, folder string) ([]string, error) {
	data, err := c.Request(ctx, http.MethodGet, "/models/"+folder, nil, nil, localhttp.MaxJSONBytes)
	var pe *modeladapter.ProviderError
	if errors.As(err, &pe) && pe.Status == http.StatusNotFound {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	files := []string{}
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, c.Error(http.StatusOK, "undecodable model list", err)
	}
	return files, nil
}
