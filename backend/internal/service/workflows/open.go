package workflows

import (
	"context"
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
	c, err := s.client(ctx, w.Endpoint, true)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(unsafeFileChars.ReplaceAllString(w.Name, "_"))
	if name == "" {
		name = w.ID
	}
	file := openFolder + "/" + name + ".json"
	_, err = c.Request(ctx, http.MethodPost, "/userdata/"+url.PathEscape("workflows/"+file)+"?overwrite=true&full_info=false",
		map[string]string{"Content-Type": "application/json"}, []byte(w.UIJSON), 1<<20)
	if err != nil {
		return "", invalid("ComfyUI にワークフローを置けませんでした: %s", reasonOf(err))
	}
	return file, nil
}
