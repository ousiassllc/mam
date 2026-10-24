package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// remoteLimit bounds what a ComfyUI server answers to the reads below (object_info of a server with many custom
// nodes is several megabytes).
const remoteLimit = localhttp.MaxJSONBytes

// client is the HTTP client of the ComfyUI endpoint. With start, the bundled ComfyUI is started first when it is
// stopped (what a run needs); without, a stopped engine is simply not reachable.
func (s *Service) client(ctx context.Context, endpoint string, start bool) (*localhttp.Client, error) {
	if endpoint == BundledEndpoint && start && s.Engine != nil {
		if _, err := s.Engine.StartEngine(ctx); err != nil {
			switch {
			case errors.Is(err, modelmanager.ErrEngineNotInstalled):
				return nil, invalid("同梱の ComfyUI がまだ入っていません。モデルマネージャーで入れてください")
			case errors.Is(err, modelmanager.ErrUnsupported):
				return nil, invalid("同梱の ComfyUI は Windows 版だけで使えます。外部の ComfyUI を接続先に登録してください")
			}
			return nil, err
		}
	}
	ep, ok := s.Adapter.Config().Endpoints[endpoint]
	if !ok || ep.Kind != localcfg.KindComfyUI {
		if endpoint == BundledEndpoint {
			return nil, invalid("同梱の ComfyUI の接続先がありません。モデルマネージャーで ComfyUI を入れてください")
		}
		return nil, invalid("接続先 %s は ComfyUI として登録されていません（設定の「ローカル・代替モデル」で登録してください）", endpoint)
	}
	c, err := localhttp.New(endpoint, ep, s.Keys, s.StagingDir)
	if err != nil {
		return nil, invalid("接続先 %s の URL を使えません: %v", endpoint, err)
	}
	c.PollEvery = 0
	if s.Probe != nil {
		c.Requests = s.Probe
	}
	return c, nil
}

// getJSON reads path of the server into v.
func getJSON(ctx context.Context, c *localhttp.Client, path string, v any) error {
	data, err := c.Request(ctx, http.MethodGet, path, nil, nil, remoteLimit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return invalid("ComfyUI の応答（%s）を読めません。ComfyUI のバージョンが違うかもしれません", path)
	}
	return nil
}

// objectInfo is GET /object_info: every node class the server has and the inputs it takes.
type objectInfo map[string]struct {
	Input struct {
		Required map[string]json.RawMessage `json:"required"`
		Optional map[string]json.RawMessage `json:"optional"`
	} `json:"input"`
}

// choices are the options of a combo input ([[a, b, ...], {...}]), or nil when the input is not a combo.
func choices(spec json.RawMessage) (options []string, uploads bool) {
	var parts []json.RawMessage
	if json.Unmarshal(spec, &parts) != nil || len(parts) == 0 {
		return nil, false
	}
	if json.Unmarshal(parts[0], &options) != nil {
		return nil, false
	}
	if len(parts) > 1 {
		var extra struct {
			ImageUpload bool `json:"image_upload"`
			VideoUpload bool `json:"video_upload"`
		}
		_ = json.Unmarshal(parts[1], &extra)
		uploads = extra.ImageUpload || extra.VideoUpload
	}
	return options, uploads
}

// inputSpec is the declaration of an input of a node class.
func (o objectInfo) inputSpec(class, input string) (json.RawMessage, bool) {
	n, ok := o[class]
	if !ok {
		return nil, false
	}
	if v, ok := n.Input.Required[input]; ok {
		return v, true
	}
	v, ok := n.Input.Optional[input]
	return v, ok
}

// folderOf is the model folder a loader node's input reads (GET /models/<folder>).
var folderOf = map[[2]string]string{
	{"CheckpointLoaderSimple", "ckpt_name"}: "checkpoints", {"CheckpointLoader", "ckpt_name"}: "checkpoints",
	{"unCLIPCheckpointLoader", "ckpt_name"}: "checkpoints", {"ImageOnlyCheckpointLoader", "ckpt_name"}: "checkpoints",
	{"UNETLoader", "unet_name"}: "diffusion_models", {"VAELoader", "vae_name"}: "vae",
	{"CLIPLoader", "clip_name"}: "text_encoders", {"DualCLIPLoader", "clip_name1"}: "text_encoders", {"DualCLIPLoader", "clip_name2"}: "text_encoders",
	{"TripleCLIPLoader", "clip_name1"}: "text_encoders", {"CLIPVisionLoader", "clip_name"}: "clip_vision",
	{"LoraLoader", "lora_name"}: "loras", {"LoraLoaderModelOnly", "lora_name"}: "loras",
	{"ControlNetLoader", "control_net_name"}: "controlnet", {"UpscaleModelLoader", "model_name"}: "upscale_models",
	{"StyleModelLoader", "style_model_name"}: "style_models",
}

// modelFolder is GET /models/<folder>: the files the server has in the folder.
func modelFolder(ctx context.Context, c *localhttp.Client, folder string) ([]string, error) {
	var names []string
	err := getJSON(ctx, c, "/models/"+url.PathEscape(folder), &names)
	return names, err
}
