package comfyuitest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Server is what the fake answers on the routes the node screen and the workflow service read: GET /system_stats,
// /object_info, /models/<folder>, /userdata (the saved workflows), GET /history (the runs) and "/" (the page the
// iframe loads). Zero values answer an empty server.
type Server struct {
	// Version is system.comfyui_version ("" answers "0.42.1").
	Version string
	// ObjectInfo is the answer of GET /object_info (a JSON object; nil: {}).
	ObjectInfo json.RawMessage
	// Models are the files GET /models/<folder> lists; an unknown folder answers 404.
	Models map[string][]string
	// UserData are the files of the user data by path under "workflows/" (for GET /userdata?dir=workflows and
	// GET /userdata/workflows%2F<path>); POSTed files are added.
	UserData map[string]string
	// HistoryList is the "prompt" tuple of each run GET /history lists: [number, prompt_id, graph, extra_data, outputs].
	HistoryList []json.RawMessage
	// RejectCrossSite answers 403 to a request that carries Sec-Fetch-Site: cross-site (ComfyUI's own origin check).
	RejectCrossSite bool
	// Down makes every route answer 503.
	Down bool
}

// serveServer answers the routes of Server; it reports whether it did.
func (f *Fake) serveServer(w http.ResponseWriter, r *http.Request) bool {
	f.ServerMu.Lock()
	defer f.ServerMu.Unlock()
	s := &f.Server
	if s.Down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return true
	}
	if s.RejectCrossSite && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	p := r.URL.Path
	switch {
	case p == "/" && r.Method == http.MethodGet:
		_, _ = w.Write([]byte("<!doctype html><title>ComfyUI</title>"))
	case p == "/system_stats":
		version := s.Version
		if version == "" {
			version = "0.42.1"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"system": map[string]any{"comfyui_version": version}, "devices": []map[string]any{{"name": "fake gpu"}}})
	case p == "/object_info":
		if s.ObjectInfo == nil {
			_, _ = w.Write([]byte("{}"))
			return true
		}
		_, _ = w.Write(s.ObjectInfo)
	case strings.HasPrefix(p, "/models/"):
		files, ok := s.Models[strings.TrimPrefix(p, "/models/")]
		if !ok {
			http.NotFound(w, r)
			return true
		}
		_ = json.NewEncoder(w).Encode(files)
	case p == "/history" && r.Method == http.MethodGet:
		out := map[string]any{}
		for _, raw := range s.HistoryList {
			var tuple []json.RawMessage
			if json.Unmarshal(raw, &tuple) != nil || len(tuple) < 2 {
				continue
			}
			var id string
			_ = json.Unmarshal(tuple[1], &id)
			out[id] = map[string]any{"prompt": tuple, "status": map[string]any{"status_str": "success", "completed": true}}
		}
		_ = json.NewEncoder(w).Encode(out)
	case p == "/userdata" && r.Method == http.MethodGet:
		list := []map[string]any{}
		for path, body := range s.UserData {
			list = append(list, map[string]any{"path": path, "size": len(body), "modified": 1.0})
		}
		_ = json.NewEncoder(w).Encode(list)
	case strings.HasPrefix(p, "/userdata/"):
		file := strings.TrimPrefix(strings.TrimPrefix(p, "/userdata/"), "workflows/")
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			if s.UserData == nil {
				s.UserData = map[string]string{}
			}
			s.UserData[file] = string(body)
			_ = json.NewEncoder(w).Encode(file)
			return true
		}
		saved, ok := s.UserData[file]
		if !ok {
			http.NotFound(w, r)
			return true
		}
		_, _ = w.Write([]byte(saved))
	default:
		return false
	}
	return true
}
