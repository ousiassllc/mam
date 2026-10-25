package hftest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("search"))
	tag := r.URL.Query().Get("pipeline_tag")
	s.mu.Lock()
	var hits []Repo
	for _, repo := range s.repos {
		if strings.Contains(strings.ToLower(repo.ID), q) && (tag == "" || repo.PipelineTag == tag) {
			hits = append(hits, repo)
		}
	}
	s.mu.Unlock()
	sort.Slice(hits, func(i, j int) bool { return hits[i].Downloads > hits[j].Downloads })
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{"id": h.ID, "downloads": h.Downloads, "likes": h.Likes, "gated": gatedField(h), "tags": tagsOf(h), "pipeline_tag": h.PipelineTag})
	}
	writeJSON(w, http.StatusOK, out)
}

func gatedField(r Repo) any {
	if r.Gated {
		return "auto"
	}
	return false
}

func tagsOf(r Repo) []string {
	if r.NFAA {
		return []string{"safetensors", "not-for-all-audiences"}
	}
	return []string{"safetensors"}
}

// api serves /api/models/<owner>/<name>/<revision|tree|paths-info>/<rev>.
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/models/"), "/")
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}
	id := parts[0] + "/" + parts[1]
	s.mu.Lock()
	to, renamed := s.renames[id]
	repo, ok := s.repos[id]
	s.mu.Unlock()
	if renamed {
		w.Header().Set("Location", "/api/models/"+url.PathEscape(strings.Split(to, "/")[0])+"/"+url.PathEscape(strings.Split(to, "/")[1])+"/"+strings.Join(parts[2:], "/")+"?"+r.URL.RawQuery)
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	}
	if !ok {
		w.Header().Set("X-Error-Code", "RepoNotFound")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid username or password."})
		return
	}
	if rev := parts[3]; rev != "main" && rev != repo.Commit && (repo.OldCommit == "" || rev != repo.OldCommit) {
		w.Header().Set("X-Error-Code", "RevisionNotFound")
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Revision not found"})
		return
	}
	switch parts[2] {
	case "revision":
		if parts[3] == "main" && repo.HeadMalformed {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id": "`))
			return
		}
		if parts[3] == "main" && repo.HeadStatus != 0 {
			writeJSON(w, repo.HeadStatus, map[string]string{"error": "the head cannot be read"})
			return
		}
		s.revision(w, repo, parts[3])
	case "tree":
		s.tree(w, r, repo)
	case "paths-info":
		s.pathsInfo(w, r, repo)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) revision(w http.ResponseWriter, repo Repo, rev string) {
	card := map[string]any{}
	if repo.License != "" {
		card["license"] = repo.License
	}
	if repo.LicenseName != "" {
		card["license_name"] = repo.LicenseName
	}
	if repo.LicenseLink != "" {
		card["license_link"] = repo.LicenseLink
	}
	sha, tags := repo.Commit, tagsOf(repo)
	if repo.OldCommit != "" && rev == repo.OldCommit { // an earlier commit: the card had no NFAA tag yet
		sha, tags = repo.OldCommit, []string{"safetensors"}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": repo.ID, "sha": sha, "gated": gatedField(repo), "private": false, "disabled": repo.Disabled, "tags": tags, "cardData": card,
	})
}

func entry(f File) map[string]any {
	e := map[string]any{"type": "file", "oid": "0000000000000000000000000000000000000000", "size": len(f.Body), "path": f.Path}
	if !f.NoLFS {
		e["lfs"] = map[string]any{"oid": SHA(f.Body), "size": len(f.Body), "pointerSize": 135}
	}
	return e
}

func (s *Server) tree(w http.ResponseWriter, r *http.Request, repo Repo) {
	files := append([]File(nil), repo.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	start, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	end := min(start+max(s.PageSize, 1), len(files))
	out := []map[string]any{}
	for _, f := range files[min(start, len(files)):end] {
		out = append(out, entry(f))
	}
	if end < len(files) {
		next := url.Values{"cursor": {strconv.Itoa(end)}, "recursive": {"true"}}
		w.Header().Set("Link", `<https://huggingface.co`+r.URL.Path+"?"+next.Encode()+`>; rel="next"`)
	}
	writeJSON(w, http.StatusOK, out)
}

// open reports whether the request may read the files of a gated repository: the token is the right one and was granted access.
func (s *Server) open(r *http.Request, repo Repo) bool {
	return !repo.Gated || (s.authorized(r) && s.Granted)
}

func (s *Server) pathsInfo(w http.ResponseWriter, r *http.Request, repo Repo) {
	if !s.open(r, repo) { // as the Hub: 401 until access is granted
		w.Header().Set("X-Error-Code", "GatedRepo")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Access to model " + repo.ID + " is restricted."})
		return
	}
	var in struct {
		Paths  []string `json:"paths"`
		Expand bool     `json:"expand"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := []map[string]any{}
	for _, p := range in.Paths {
		for _, f := range repo.Files {
			if f.Path != p {
				continue
			}
			e := entry(f)
			if in.Expand {
				status := f.Security
				if status == "" {
					status = "safe"
				}
				e["securityFileStatus"] = map[string]any{"status": status}
			}
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// resolve serves /<owner>/<name>/resolve/<commit>/<path>: a 302 to the CDN (a gated repository is 401 without access).
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 5)
	if len(parts) < 5 || parts[2] != "resolve" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	repo, ok := s.repos[parts[0]+"/"+parts[1]]
	s.mu.Unlock()
	if !ok || (parts[3] != "main" && parts[3] != repo.Commit && (repo.OldCommit == "" || parts[3] != repo.OldCommit)) {
		http.NotFound(w, r)
		return
	}
	if !s.open(r, repo) {
		w.Header().Set("X-Error-Code", "GatedRepo")
		status := http.StatusUnauthorized
		if s.authorized(r) {
			status = http.StatusForbidden
		}
		http.Error(w, "Access to model "+repo.ID+" is restricted.", status)
		return
	}
	for i, f := range repo.Files {
		if f.Path == parts[4] {
			w.Header().Set("Location", "http://"+s.CDNHost()+"/cdn/"+repo.ID+"/"+strconv.Itoa(i))
			w.WriteHeader(http.StatusFound)
			return
		}
	}
	w.Header().Set("X-Error-Code", "EntryNotFound")
	http.NotFound(w, r)
}

func (s *Server) cdn(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/cdn/")
	i := strings.LastIndex(rest, "/")
	idx, err := strconv.Atoi(rest[i+1:])
	s.mu.Lock()
	repo, ok := s.repos[rest[:max(i, 0)]]
	s.mu.Unlock()
	if err != nil || !ok || idx >= len(repo.Files) {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	hold := s.hold
	s.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-r.Context().Done():
			return
		}
	}
	body := repo.Files[idx].Body
	if repo.Files[idx].Served != nil {
		body = repo.Files[idx].Served
	}
	http.ServeContent(w, r, "f", time.Time{}, strings.NewReader(string(body)))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
