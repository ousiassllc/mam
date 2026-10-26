package hf

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// TagNFAA is the tag of a repository that marks adult content ("not-for-all-audiences").
const TagNFAA = "not-for-all-audiences"

// PipelineTags are the kinds of the search filter (the pipeline_tag of the Hub).
var PipelineTags = []string{"text-to-image", "text-to-video", "image-to-video"}

// SearchOptions is a search of the models of the Hub, most downloaded first.
type SearchOptions struct {
	Query string
	// PipelineTag narrows the search to one of PipelineTags ("" for all).
	PipelineTag string
	// Limit is the number of results (default 20, at most 50).
	Limit int
}

// SearchHit is a model of a search result.
type SearchHit struct {
	Repo        string `json:"repo"`
	Downloads   int64  `json:"downloads"`
	Likes       int64  `json:"likes"`
	Gated       bool   `json:"gated"`
	NFAA        bool   `json:"nfaa"`
	PipelineTag string `json:"pipeline_tag,omitempty"`
}

// Search lists the models that match the query, most downloaded first. NFAA results are marked, not removed: the
// service decides (the age gate).
func (c *Client) Search(ctx context.Context, o SearchOptions) ([]SearchHit, error) {
	limit := o.Limit
	if limit <= 0 {
		limit = 20
	}
	q := url.Values{"search": {o.Query}, "sort": {"downloads"}, "limit": {strconv.Itoa(min(limit, 50))}}
	q["expand[]"] = []string{"downloads", "likes", "gated", "tags", "pipeline_tag"}
	if o.PipelineTag != "" {
		q.Set("pipeline_tag", o.PipelineTag)
	}
	var raw []struct {
		ID          string          `json:"id"`
		Downloads   int64           `json:"downloads"`
		Likes       int64           `json:"likes"`
		Gated       json.RawMessage `json:"gated"`
		Tags        []string        `json:"tags"`
		PipelineTag string          `json:"pipeline_tag"`
	}
	if _, err := c.getJSON(ctx, request{method: http.MethodGet, path: "/api/models", query: q}, &raw); err != nil {
		return nil, err
	}
	hits := make([]SearchHit, 0, len(raw))
	for _, m := range raw {
		if !ValidRepo(m.ID) {
			continue
		}
		hits = append(hits, SearchHit{
			Repo: m.ID, Downloads: m.Downloads, Likes: m.Likes, Gated: isGated(m.Gated),
			NFAA: IsNFAA(m.Tags), PipelineTag: m.PipelineTag,
		})
	}
	return hits, nil
}

// isGated reads the "gated" field: false, or "auto" / "manual".
func isGated(raw json.RawMessage) bool {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s != "" && s != "false"
	}
	var b bool
	return json.Unmarshal(raw, &b) == nil && b
}

// Repo is the information of a repository at a commit.
type Repo struct {
	// ID is the canonical "owner/name" (a renamed repository answers with its new name).
	ID string `json:"id"`
	// Commit is the full hash the revision resolved to; every later request is pinned to it.
	Commit string `json:"commit"`
	Gated  bool   `json:"gated"`
	NFAA   bool   `json:"nfaa"`
	// Disabled is a repository the Hub switched off.
	Disabled bool     `json:"disabled,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	License  License  `json:"license"`
}

// License is what the model card says of the license (any field may be empty).
type License struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Link string `json:"link,omitempty"`
}

// Unclear reports whether the card gives no usable license: none, or "other" without a name.
func (l License) Unclear() bool {
	return l.ID == "" || (strings.EqualFold(l.ID, "other") && l.Name == "" && l.Link == "")
}

// IsNFAA reports whether the tags mark adult content: the tag of the Hub, in any case, or its short form "nfaa".
func IsNFAA(tags []string) bool {
	return slices.ContainsFunc(tags, func(t string) bool {
		return strings.EqualFold(t, TagNFAA) || strings.EqualFold(t, "nfaa")
	})
}

// revisionAnswer is the answer of /api/models/<repo>/revision/<rev>.
type revisionAnswer struct {
	ID       string          `json:"id"`
	SHA      string          `json:"sha"`
	Gated    json.RawMessage `json:"gated"`
	Disabled bool            `json:"disabled"`
	Tags     []string        `json:"tags"`
	CardData struct {
		License     json.RawMessage `json:"license"`
		LicenseName string          `json:"license_name"`
		LicenseLink string          `json:"license_link"`
	} `json:"cardData"`
}

func (c *Client) revision(ctx context.Context, repo, rev string) (revisionAnswer, error) {
	var raw revisionAnswer
	_, err := c.getJSON(ctx, request{method: http.MethodGet, path: apiPath(repo, "revision", rev)}, &raw)
	return raw, err
}

// Revision reads the repository at rev ("" is main) and pins it to the commit. A repository that was renamed is
// followed (the Hub answers 307) and ID is its new name. NFAA is the tag at the commit or at the repository's current
// head (an older commit of a repository that is NFAA today may not have the tag yet, so naming it never opens the gate).
func (c *Client) Revision(ctx context.Context, repo, rev string) (Repo, error) {
	if rev == "" {
		rev = "main"
	}
	raw, err := c.revision(ctx, repo, rev)
	if err != nil {
		return Repo{}, err
	}
	nfaa := IsNFAA(raw.Tags)
	if rev != "main" {
		head, err := c.revision(ctx, repo, "main")
		if err != nil {
			return Repo{}, err
		}
		nfaa = nfaa || IsNFAA(head.Tags)
	}
	r := Repo{
		ID: raw.ID, Commit: raw.SHA, Gated: isGated(raw.Gated), Disabled: raw.Disabled, Tags: raw.Tags,
		NFAA: nfaa, License: License{ID: licenseID(raw.CardData.License), Name: raw.CardData.LicenseName, Link: raw.CardData.LicenseLink},
	}
	if r.ID == "" {
		r.ID = repo
	}
	if !ValidRepo(r.ID) || !IsCommit(r.Commit) {
		return Repo{}, &APIError{Status: http.StatusBadGateway, Message: "the answer has no repository id or commit"}
	}
	if r.License.ID == "" { // the tag license:<id> is there when the card's front matter was not read
		for _, t := range raw.Tags {
			if id, ok := strings.CutPrefix(t, "license:"); ok {
				r.License.ID = id
			}
		}
	}
	return r, nil
}

// licenseID reads cardData.license: a string, or (rarely) a list of strings (the first is taken).
func licenseID(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		return list[0]
	}
	return ""
}

// TreeEntry is a file of a repository at a commit. LFS is nil for a file that is not in Git LFS (its SHA-256 is unknown).
type TreeEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	// SHA256 is the LFS oid ("" when the file is not an LFS file); LFSSize is the size of the LFS object.
	SHA256  string `json:"sha256,omitempty"`
	LFSSize int64  `json:"lfs_size,omitempty"`
}

// MaxTreeEntries bounds how many entries Tree reads (a repository with more is cut and Truncated says so).
const MaxTreeEntries = 5000

// maxTreePages bounds how many pages Tree follows: a server that keeps answering a next cursor with no files, or one that repeats
// a cursor, ends the list there (truncated).
const maxTreePages = 100

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// lfsOID is the SHA-256 the Hub gives as an LFS object id, lower-cased; "" when it is not 64 hex digits.
func lfsOID(oid string) string {
	if oid = strings.ToLower(oid); sha256Hex.MatchString(oid) {
		return oid
	}
	return ""
}

// Tree lists the files of the repository at the commit, directories excluded, following the pages the Hub makes
// (the cursor of the Link header). truncated is true when MaxTreeEntries cut the list.
func (c *Client) Tree(ctx context.Context, repo, commit string) (entries []TreeEntry, truncated bool, err error) {
	q := url.Values{"recursive": {"true"}, "limit": {"1000"}}
	for pages := 1; ; pages++ {
		var page []struct {
			Type string `json:"type"`
			Path string `json:"path"`
			Size int64  `json:"size"`
			LFS  *struct {
				OID  string `json:"oid"`
				Size int64  `json:"size"`
			} `json:"lfs"`
		}
		h, err := c.getJSON(ctx, request{method: http.MethodGet, path: apiPath(repo, "tree", commit), query: q}, &page)
		if err != nil {
			return nil, false, err
		}
		for _, e := range page {
			if e.Type != "file" || !ValidPath(e.Path) {
				continue
			}
			te := TreeEntry{Path: e.Path, Size: e.Size}
			if e.LFS != nil {
				te.SHA256, te.LFSSize = lfsOID(e.LFS.OID), e.LFS.Size
				if te.SHA256 == "" {
					te.LFSSize = 0 // not a SHA-256: the file counts as one without LFS
				}
			}
			entries = append(entries, te)
		}
		cursor := nextCursor(h.Get("Link"))
		if cursor == "" {
			return entries, false, nil
		}
		if len(entries) >= MaxTreeEntries || pages >= maxTreePages || cursor == q.Get("cursor") {
			return entries, true, nil
		}
		q.Set("cursor", cursor)
	}
}

// nextCursor is the cursor of the rel="next" link of a Link header ("" for the last page). Only the cursor is taken,
// never the URL: the next request is always built from the client's own base.
func nextCursor(link string) string {
	for _, part := range strings.Split(link, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start, end := strings.Index(part, "<"), strings.Index(part, ">")
		if start < 0 || end < start {
			continue
		}
		if u, err := url.Parse(part[start+1 : end]); err == nil {
			return u.Query().Get("cursor")
		}
	}
	return ""
}
