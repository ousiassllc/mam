package modelmanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
)

// modelFolders are the ComfyUI model folders the catalog installs into.
var modelFolders = []string{"checkpoints", "diffusion_models", "text_encoders", "vae", "loras", "latent_upscale_models"}

// ExternalModel is a model of the catalog against an external ComfyUI: which of its files the server has.
type ExternalModel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Present []string `json:"present"`
	Missing []string `json:"missing"`
	// Complete: the server has every file; the model works there once a workflow is registered on the endpoint.
	Complete bool `json:"complete"`
}

// ExternalInventory is what an external ComfyUI has installed.
type ExternalInventory struct {
	Endpoint string `json:"endpoint"`
	Version  string `json:"version,omitempty"`
	// Folders lists the files of each model folder (GET /models/<folder>).
	Folders map[string][]string `json:"folders"`
	Models  []ExternalModel     `json:"models"`
}

// ExternalInventory asks an external ComfyUI endpoint (kind comfyui, other than the bundled one) which model files
// it has, and compares them with the catalog: a model is shown as missing there. ComfyUI has no API to download
// models into a server, so mam installs nothing on an external server (spec/architecture/model-manager/model-manager.md §1).
func (s *Service) ExternalInventory(ctx context.Context, endpoint string) (ExternalInventory, error) {
	ep, ok := s.Adapter.Config().Endpoints[endpoint]
	if !ok {
		return ExternalInventory{}, ErrNotFound
	}
	if ep.Kind != localcfg.KindComfyUI || endpoint == BundledEndpoint {
		return ExternalInventory{}, invalid("外部の ComfyUI の接続先を指定してください")
	}
	client, err := localhttp.New(endpoint, ep, s.Keys, s.Backends.StagingDir)
	if err != nil {
		return ExternalInventory{}, invalid("接続先を使えません: %v", err)
	}
	inv := ExternalInventory{Endpoint: endpoint, Folders: map[string][]string{}, Models: []ExternalModel{}}
	var stats struct {
		System struct {
			Version string `json:"comfyui_version"`
		} `json:"system"`
	}
	data, err := client.Request(ctx, http.MethodGet, "/system_stats", nil, nil, localhttp.MaxJSONBytes)
	if err != nil {
		return inv, err
	}
	_ = json.Unmarshal(data, &stats)
	inv.Version = stats.System.Version
	for _, folder := range modelFolders {
		files, err := listFolder(ctx, client, folder)
		if err != nil {
			return inv, err
		}
		inv.Folders[folder] = files
	}
	for _, m := range s.cat.Models {
		if !s.allowed(m) { // an adult model is not listed until the age confirmation is on record
			continue
		}
		em := ExternalModel{ID: m.ID, Name: m.Name, Present: []string{}, Missing: []string{}}
		for _, f := range m.Files {
			if slices.Contains(inv.Folders[f.Folder], f.Name) {
				em.Present = append(em.Present, f.Folder+"/"+f.Name)
			} else {
				em.Missing = append(em.Missing, f.Folder+"/"+f.Name)
			}
		}
		em.Complete = len(em.Missing) == 0
		inv.Models = append(inv.Models, em)
	}
	return inv, nil
}

// listFolder is GET /models/<folder>; a folder the server does not have (404) is empty.
func listFolder(ctx context.Context, c *localhttp.Client, folder string) ([]string, error) {
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
