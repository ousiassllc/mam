package ollamapull

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/httpclient"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func defaultFree(path string) (uint64, error) { return fsutil.FreeBytes(path) }

// free is the free space of the disk of the models.
func (s *Service) free() (uint64, error) {
	free := s.FreeBytes
	if free == nil {
		free = defaultFree
	}
	return free(s.layout.Root)
}

// Delete removes a model that was fetched: Ollama's DELETE /api/delete (it removes the manifest and the layers no other model uses). It refuses a name that is not in the list (*InvalidError), a server
// that is not installed or cannot run here (the errors of localruntime), a model that is being pulled (modelmanager.ErrBusy) or is not fetched (modelmanager.ErrNotInstalled), and a model that the LLM
// assignment of an Agent chooses (*InvalidError that names the Agents): nothing is removed then. A server that is stopped is started for the delete and stopped again.
func (s *Service) Delete(ctx context.Context, name string) error {
	m, err := s.find(name)
	if err != nil {
		return err
	}
	rv, err := s.Runtime.Status(ctx, RuntimeID)
	if err != nil {
		return err
	}
	switch server, _ := serverOf(rv); server {
	case ServerUnsupported:
		return &localruntime.SupportError{Runtime: RuntimeID, Support: rv.Support}
	case ServerNotInstalled:
		return localruntime.ErrNotInstalled
	case ServerInstalling:
		return localruntime.ErrBusy
	}
	if s.activeJob(TargetPrefix+m.Name) != "" {
		return modelmanager.ErrBusy
	}
	if s.CheckManifest(m) == CheckMissing {
		return modelmanager.ErrNotInstalled
	}
	if used := s.usedBy(m.Name); len(used) > 0 {
		return &InvalidError{Message: fmt.Sprintf("「%s」は、LLM の割り当てで使われています（%s）。先に割り当てを変えてください", m.DisplayName, strings.Join(used, "、"))}
	}
	baseURL := processURL(rv)
	if baseURL == "" { // stopped: it runs for the delete only
		if err := s.Runtime.Prepare(ctx, RuntimeID); err != nil {
			return s.startFailure(ctx, err)
		}
		defer func() { _ = s.Runtime.Stop(RuntimeID) }()
		if rv, err = s.Runtime.Status(ctx, RuntimeID); err != nil {
			return err
		}
		if baseURL = processURL(rv); baseURL == "" {
			return &userError{msg: "Ollama が起動していません" + logSuffix(rv.LogTail)}
		}
	}
	return s.deleteOn(ctx, baseURL, m.Name)
}

func (s *Service) usedBy(model string) []string {
	if s.UsedBy == nil {
		return nil
	}
	return s.UsedBy(model)
}

// deleteOn asks the server at baseURL to delete the model (DELETE /api/delete).
func (s *Service) deleteOn(ctx context.Context, baseURL, model string) error {
	body, err := json.Marshal(map[string]string{"model": model})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.HTTP.Do(req)
	if err != nil {
		return &userError{msg: "Ollama に接続できませんでした", err: httpclient.RedactURL(err)}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &userError{msg: "Ollama がモデルを削除できませんでした（" + extractError(data, res.Status) + "）", err: fmt.Errorf("ollama: /api/delete answered %s", res.Status)}
	}
	return nil
}
