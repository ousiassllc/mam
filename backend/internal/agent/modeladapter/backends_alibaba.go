package modeladapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// alibabaBaseURL is the Singapore region of Alibaba Cloud Model Studio, on the workspace-dedicated domain the Wan 3.0 API
// reference documents (spec/architecture/model-adapters.md §9.4). {WorkspaceId} is the workspace of the user: the key holds it.
const alibabaBaseURL = "https://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com"

const alibabaWorkspacePlaceholder = "{WorkspaceId}"

// alibabaWorkspaceID is what a workspace ID may be: one label of a host name (it is put in the host of the URL).
var alibabaWorkspaceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// alibabaBackend calls Wan 3.0 (wan3.0-video) through the video-synthesis API of Model Studio: an asynchronous task created
// with X-DashScope-Async, polled with GET /api/v1/tasks/{task_id}.
type alibabaBackend struct{ api }

// resolve returns the API with its base URL filled in, and the API key: when the base URL has {WorkspaceId} the stored key is
// WORKSPACE_ID:API_KEY (like the Kling 3.0 key), else (a base URL set to a host of the user's own) it is the API key itself.
func (b alibabaBackend) resolve() (api, string, error) {
	key, err := b.key()
	if err != nil {
		return api{}, "", err
	}
	a := b.api
	if !strings.Contains(a.baseURL, alibabaWorkspacePlaceholder) {
		return a, key, nil
	}
	workspace, key, ok := strings.Cut(key, ":")
	if !ok || key == "" || !alibabaWorkspaceID.MatchString(workspace) {
		return api{}, "", fmt.Errorf("%w: the alibaba key must be WORKSPACE_ID:API_KEY", ErrNoAPIKey)
	}
	a.baseURL = strings.ReplaceAll(a.baseURL, alibabaWorkspacePlaceholder, workspace)
	return a, key, nil
}

// body is the create call. Text-to-video has the prompt only (no media); a first frame, with a last frame when there is one,
// is image-to-video; reference images are reference-to-video. The API rejects frames and references in one request (Prepare
// keeps one of the two). An image-to-video output keeps the ratio of the image (adaptive).
func (b alibabaBackend) body(apiModel string, req VideoRequest) map[string]any {
	var media []map[string]string
	image := func(kind string, in Input) {
		media = append(media, map[string]string{"type": kind, "url": "data:" + in.MimeType + ";base64," + base64.StdEncoding.EncodeToString(in.Data)})
	}
	ratio := req.AspectRatio
	switch {
	case req.FirstFrame != nil:
		image("first_frame", *req.FirstFrame)
		if req.LastFrame != nil {
			image("last_frame", *req.LastFrame)
		}
		ratio = "adaptive"
	case len(req.References) > 0:
		for _, r := range req.References {
			image("reference_image", r)
		}
	}
	input := map[string]any{"prompt": req.Prompt}
	if media != nil {
		input["media"] = media
	}
	return map[string]any{
		"model": apiModel, "input": input,
		"parameters": map[string]any{"resolution": strings.ToUpper(req.Resolution), "ratio": ratio, "duration": int(req.DurationSeconds), "audio": true},
	}
}

func (b alibabaBackend) GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error) {
	a, key, err := b.resolve()
	if err != nil {
		return VideoResult{}, err
	}
	auth := map[string]string{"Authorization": "Bearer " + key}

	raw, _ := json.Marshal(b.body(apiModel, req))
	header := jsonHeader(auth)
	header["X-DashScope-Async"] = "enable"
	data, err := a.request(ctx, http.MethodPost, a.baseURL+"/api/v1/services/aigc/video-generation/video-synthesis", header, raw, maxJSONBytes)
	if err != nil {
		return VideoResult{}, err
	}
	var submit struct {
		Output struct {
			TaskID string `json:"task_id"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := unmarshal(a, data, &submit); err != nil {
		return VideoResult{}, err
	}
	if submit.Output.TaskID == "" {
		if submit.Code != "" {
			return VideoResult{}, a.failed("the video job was not accepted: " + submit.Code + ": " + submit.Message)
		}
		return VideoResult{}, a.empty("task id")
	}

	var videoURL string
	err = a.poll(ctx, func(ctx context.Context) (bool, error) {
		d, err := a.request(ctx, http.MethodGet, a.baseURL+"/api/v1/tasks/"+url.PathEscape(submit.Output.TaskID), auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st struct {
			Output struct {
				Status   string `json:"task_status"`
				VideoURL string `json:"video_url"`
				Code     string `json:"code"`
				Message  string `json:"message"`
			} `json:"output"`
		}
		if err := unmarshal(a, d, &st); err != nil {
			return false, err
		}
		switch out := st.Output; out.Status {
		case "SUCCEEDED":
			if out.VideoURL == "" {
				return false, a.empty("video")
			}
			videoURL = out.VideoURL
			return true, nil
		case "FAILED", "CANCELED", "UNKNOWN": // UNKNOWN: the task does not exist (its ID is valid for 24 hours)
			return false, a.failed("the video job ended with status " + out.Status + ": " + out.Code + ": " + out.Message)
		}
		return false, nil // PENDING, RUNNING
	})
	if err != nil {
		return VideoResult{}, err
	}
	file, mime, err := a.download(ctx, videoURL)
	if err != nil {
		return VideoResult{}, err
	}
	return VideoResult{File: file, MimeType: videoMime(mime)}, nil
}
