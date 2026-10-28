package modeladapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// omniReferenceGuide is the closing sentence the Omni guide asks for with reference images, so that they are not used as
// literal first frames (https://ai.google.dev/gemini-api/docs/omni, "Declaring sources and references").
const omniReferenceGuide = " Use the given image(s) as references for video generation. The images should not be used as literal initial frames."

// geminiOmniBackend calls Gemini Omni Flash through the Interactions API (spec/architecture/model-adapters.md §9.1): the clip
// is asked for with POST /v1beta/interactions, delivered as a file of the Files API that is polled until it is ACTIVE and then
// downloaded. An answer that carries the video inline (data) is read as it is.
type geminiOmniBackend struct{ api }

// omniVideo is the video of an interaction: inline data or the URI of a file.
type omniVideo struct {
	Mime string `json:"mime_type"`
	Data string `json:"data"`
	URI  string `json:"uri"`
}

type omniInteraction struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Steps  []struct {
		Content []struct {
			Type string `json:"type"`
			omniVideo
		} `json:"content"`
	} `json:"steps"`
}

// video is the first video of the model's output.
func (i omniInteraction) video() (omniVideo, bool) {
	for _, s := range i.Steps {
		for _, c := range s.Content {
			if c.Type == "video" && (c.Data != "" || c.URI != "") {
				return c.omniVideo, true
			}
		}
	}
	return omniVideo{}, false
}

// prompt binds the images to their roles with the tags of the guide: <FIRST_FRAME> / <LAST_FRAME> for the frames, a
// "[# References ...]" declaration for free reference images.
func (b geminiOmniBackend) prompt(req VideoRequest) string {
	switch {
	case req.FirstFrame != nil && req.LastFrame != nil:
		return "<FIRST_FRAME> <LAST_FRAME> " + req.Prompt
	case req.FirstFrame != nil:
		return "<FIRST_FRAME> " + req.Prompt
	case len(req.References) > 0:
		tags := make([]string, len(req.References))
		for i := range req.References {
			tags[i] = "<IMAGE_REF_" + strconv.Itoa(i) + ">@Image" + strconv.Itoa(i+1)
		}
		return "[# References " + strings.Join(tags, " ") + "] " + req.Prompt + omniReferenceGuide
	}
	return req.Prompt
}

func (b geminiOmniBackend) body(apiModel string, req VideoRequest) ([]byte, error) {
	input := []map[string]any{}
	image := func(in Input) {
		input = append(input, map[string]any{"type": "image", "mime_type": in.MimeType, "data": base64.StdEncoding.EncodeToString(in.Data)})
	}
	task := "text_to_video"
	switch {
	case req.FirstFrame != nil:
		image(*req.FirstFrame)
		if req.LastFrame != nil {
			image(*req.LastFrame)
			task = "" // the interpolation of two frames has no task of its own: the tags say it
		} else {
			task = "image_to_video"
		}
	case len(req.References) > 0:
		for _, r := range req.References {
			image(r)
		}
		task = "reference_to_video"
	}
	input = append(input, map[string]any{"type": "text", "text": b.prompt(req)})
	body := map[string]any{
		"model": apiModel,
		"input": input,
		"response_format": map[string]any{
			"type": "video", "aspect_ratio": req.AspectRatio, "resolution": req.Resolution, "delivery": "uri",
		},
	}
	if task != "" {
		body["generation_config"] = map[string]any{"video_config": map[string]any{"task": task}}
	}
	return json.Marshal(body)
}

func (b geminiOmniBackend) GenerateVideo(ctx context.Context, apiModel string, req VideoRequest) (VideoResult, error) {
	key, err := b.key()
	if err != nil {
		return VideoResult{}, err
	}
	if req.AspectRatio != domain.AspectRatio16x9 && req.AspectRatio != domain.AspectRatio9x16 {
		// The request itself is unusable for this model: the Adapter routes to the alternative.
		return VideoResult{}, &ProviderError{Vendor: string(b.vendor), Status: http.StatusUnprocessableEntity,
			Message: "aspect ratio " + req.AspectRatio + " is not available for this model"}
	}
	auth := map[string]string{"x-goog-api-key": key}
	raw, err := b.body(apiModel, req)
	if err != nil {
		return VideoResult{}, err
	}
	data, err := b.request(ctx, http.MethodPost, b.baseURL+"/v1beta/interactions", jsonHeader(auth), raw, maxJSONBytes)
	if err != nil {
		return VideoResult{}, err
	}
	var out omniInteraction
	if err := unmarshal(b.api, data, &out); err != nil {
		return VideoResult{}, err
	}
	if out, err = b.finished(ctx, auth, out); err != nil {
		return VideoResult{}, err
	}
	video, _ := out.video()
	return b.fetchVideo(ctx, auth, video)
}

// finished waits for the interaction when the answer is not final yet (a job that runs in the background) and returns it once
// it holds a video. A final answer without one was filtered or failed.
func (b geminiOmniBackend) finished(ctx context.Context, auth map[string]string, out omniInteraction) (omniInteraction, error) {
	err := b.poll(ctx, func(ctx context.Context) (bool, error) {
		if _, ok := out.video(); ok {
			return true, nil
		}
		switch out.Status {
		case "failed", "cancelled", "incomplete":
			return false, b.failed("the video job ended with status " + out.Status)
		case "completed", "":
			return false, b.empty("video (the prompt may have been filtered)")
		}
		if out.ID == "" {
			return false, b.empty("interaction id")
		}
		d, err := b.request(ctx, http.MethodGet, b.baseURL+"/v1beta/interactions/"+url.PathEscape(out.ID), auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var next omniInteraction
		if err := unmarshal(b.api, d, &next); err != nil {
			return false, err
		}
		next.ID = out.ID
		out = next
		_, ok := out.video()
		return ok, nil
	})
	return out, err
}

// fetchVideo returns the video: decoded when it came inline, else downloaded once its file is ACTIVE.
func (b geminiOmniBackend) fetchVideo(ctx context.Context, auth map[string]string, v omniVideo) (VideoResult, error) {
	if v.Data != "" {
		file, err := base64.StdEncoding.DecodeString(v.Data)
		if err != nil || len(file) == 0 {
			return VideoResult{}, b.empty("valid video data")
		}
		return VideoResult{File: BlobOf(file), MimeType: videoMime(v.Mime)}, nil
	}
	// Only the name of the file is taken from the URI: the host of the answer is not trusted with the key, so the file is
	// asked for (status and download) at the base URL, like every other request of this backend.
	target, err := url.Parse(v.URI)
	if err != nil {
		return VideoResult{}, b.empty("file name")
	}
	name, ok := omniFileName(target.Path)
	if !ok {
		return VideoResult{}, b.empty("file name")
	}
	fileURL := b.baseURL + "/v1beta/files/" + url.PathEscape(name)
	err = b.poll(ctx, func(ctx context.Context) (bool, error) {
		d, err := b.request(ctx, http.MethodGet, fileURL, auth, nil, maxJSONBytes)
		if err != nil {
			return false, err
		}
		var st struct {
			State string `json:"state"`
		}
		if err := unmarshal(b.api, d, &st); err != nil {
			return false, err
		}
		switch st.State {
		case "ACTIVE":
			return true, nil
		case "FAILED":
			return false, b.failed("the video file could not be made")
		}
		return false, nil // PROCESSING
	})
	if err != nil {
		return VideoResult{}, err
	}
	file, mime, err := b.downloadAuthed(ctx, fileURL+":download?alt=media", auth)
	if err != nil {
		return VideoResult{}, err
	}
	return VideoResult{File: file, MimeType: videoMime(orDefault(mime, v.Mime))}, nil
}

// omniFileName is the name of a file in the path of its URI ("/v1beta/files/abc-123:download" is "abc-123").
func omniFileName(path string) (string, bool) {
	_, rest, ok := strings.Cut(path, "/files/")
	name, _, _ := strings.Cut(rest, ":")
	name = strings.Trim(name, "/")
	return name, ok && name != "" && !strings.Contains(name, "/")
}
