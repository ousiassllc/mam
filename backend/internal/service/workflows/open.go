package workflows

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// openFolder is the folder of the ComfyUI server's workflows that mam writes the workflows it opens to.
const openFolder = "mam"

var unsafeFileChars = regexp.MustCompile(`[^\p{L}\p{N} ._()-]+`)

// OpenInComfyUI writes the editor's format of the saved workflow into the ComfyUI server's own workflows
// (workflows/mam/<name>.json, the server's user data) so that the node screen can open it from the Workflows
// sidebar, and returns the name to look for there. A workflow imported from an API-format JSON alone has no
// editor format and cannot be opened (ErrNotFound is not used: the refusal says so). The bundled ComfyUI is
// started when it is stopped.
func (s *Service) OpenInComfyUI(ctx context.Context, id string) (path string, err error) {
	w, err := s.Repo.Get(ctx, id)
	if err != nil {
		return "", mapNotFound(err)
	}
	if w.UIJSON == "" {
		return "", invalid("このワークフローは API 形式の JSON だけで取り込んだため、ノード画面では開けません。ノード画面で作ったワークフローを「ComfyUI から取り込む」と、開き直せます")
	}
	name := strings.TrimSpace(unsafeFileChars.ReplaceAllString(w.Name, "_"))
	if name == "" {
		name = w.ID
	}
	return s.OpenUI(ctx, w.Endpoint, name, w.UIJSON)
}

// OpenUI puts a graph in the editor's format (ui) into the ComfyUI server's workflows as mam/<name>.json (an earlier file of the name is replaced) and
// returns the file to look for there. It is OpenInComfyUI for a graph that is not a saved workflow (Issue #977: "今の設定を ComfyUI で開く"); name is
// made a plain file name. The bundled ComfyUI is started when it is stopped.
func (s *Service) OpenUI(ctx context.Context, endpoint, name, ui string) (string, error) {
	endpoint, err := cleanEndpoint(endpoint)
	if err != nil {
		return "", err
	}
	if err := checkUI(ui); err != nil {
		return "", err
	}
	c, err := s.client(ctx, endpoint, true)
	if err != nil {
		return "", err
	}
	file := openFolder + "/" + strings.TrimSpace(unsafeFileChars.ReplaceAllString(name, "_")) + ".json"
	_, err = c.Request(ctx, http.MethodPost, "/userdata/"+url.PathEscape("workflows/"+file)+"?overwrite=true&full_info=false",
		map[string]string{"Content-Type": "application/json"}, []byte(ui), maxUIBytes)
	if err != nil {
		return "", invalid("ComfyUI にワークフローを置けませんでした: %s", reasonOf(err))
	}
	return file, nil
}

// ObjectInfo is what the ComfyUI of the endpoint says of its nodes (GET /object_info): the inputs, their types and the outputs of every class. start says
// whether a stopped bundled ComfyUI is started for it; without, a server that cannot be asked is an error.
func (s *Service) ObjectInfo(ctx context.Context, endpoint string, start bool) (json.RawMessage, error) {
	endpoint, err := cleanEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	c, err := s.client(ctx, endpoint, start)
	if err != nil {
		return nil, err
	}
	var info json.RawMessage
	if err := getJSON(ctx, c, "/object_info", &info); err != nil {
		return nil, invalid("ComfyUI のノードの情報を読めません: %s", reasonOf(err))
	}
	return info, nil
}
