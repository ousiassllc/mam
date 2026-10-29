package modelmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
)

// The guidance for a model that is behind a Hugging Face login (catalog.Model.RequiresHFToken, a gated repository). It is
// defined here once: the screen, the MCP tools, the refusal of InstallModel and the failure of a download that Hugging Face
// refuses half way all tell the user the same thing (spec/architecture/model-manager/model-manager.md §5.3).

// HFTokenSection is where the token is entered on the screen: the card "Hugging Face のトークン" of the model manager.
const HFTokenSection = "/model-manager#mm-hf"

// HFNoTokenGuide is what a model that needs the token says while none is stored (message A).
const HFNoTokenGuide = "このモデルの取得には Hugging Face のトークンが要ります。モデルマネージャーの「Hugging Face のトークン（任意）」の区画（" +
	HFTokenSection + "）でトークンを設定してください（MCP では set_huggingface_token）"

// HFInvalidTokenGuide is what Hugging Face's 401 means to the user: the stored token is not accepted (message B).
const HFInvalidTokenGuide = "Hugging Face がトークンを受け付けませんでした（無効か、期限切れです）。Hugging Face でトークンを発行し直し、「Hugging Face のトークン（任意）」の区画（" +
	HFTokenSection + "）で保存し直してください"

// hfNoAccessGuide is what Hugging Face's 403 (GatedRepo) means: the token is fine but the model's page was not agreed to, or the
// request for access is not approved yet (message C). Access is asked for on the page, never through the API.
func hfNoAccessGuide(repo, page string) string {
	return fmt.Sprintf("このトークンでは %s を取得できません。Hugging Face のモデルのページ（%s）でライセンスに同意してアクセスを申請し、承認されてからやり直してください（mam からは申請できません）", repo, page)
}

// hfDenied is the guidance for the refusal of a file of repo: 401 is a token that is not accepted (B), 403 and a GatedRepo answer
// are an access that was not granted (C).
func (s *Service) hfDenied(status int, code, repo string) string {
	if status == http.StatusUnauthorized && code != "GatedRepo" {
		return HFInvalidTokenGuide
	}
	return hfNoAccessGuide(repo, s.hf.PageURL(repo))
}

// hfFileAt splits the URL of a file of a Hugging Face repository: <base>/<owner>/<name>/resolve/<commit>/<path>.
func hfFileAt(raw string) (repo, commit, path string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", false
	}
	segs := strings.SplitN(strings.TrimPrefix(u.EscapedPath(), "/"), "/", 5)
	if len(segs) < 5 || segs[2] != "resolve" {
		return "", "", "", false
	}
	for i := range segs {
		if segs[i], err = url.PathUnescape(segs[i]); err != nil {
			return "", "", "", false
		}
	}
	return segs[0] + "/" + segs[1], segs[3], segs[4], true
}

// requireHFToken is the first check of InstallModel for a model that needs the token: none stored is message A.
func (s *Service) requireHFToken() error {
	if !s.hf.HasToken() {
		return invalid("%s", HFNoTokenGuide)
	}
	return nil
}

// checkHFAccess is the last check of InstallModel for a model that needs the token, before the job is made: the first byte of one
// file of each repository the model reads (as hfplan's checkAccess does for an import) shows whether the token is accepted (401,
// message B) and whether the access was granted (403, message C). Nothing else is downloaded.
func (s *Service) checkHFAccess(ctx context.Context, m catalog.Model) error {
	seen := map[string]bool{}
	for _, f := range m.Files {
		repo, commit, path, ok := hfFileAt(f.URL)
		if !ok || seen[repo] {
			continue
		}
		seen[repo] = true
		err := s.hf.Probe(ctx, repo, commit, path)
		var ae *hf.APIError
		switch {
		case err == nil:
		case errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden):
			return invalid("%s", s.hfDenied(ae.Status, ae.Code, repo))
		default:
			return s.hfError(err)
		}
	}
	return nil
}

// hfDownloadDenied is the failure of a download that Hugging Face refused after the job was queued (the token was cleared or
// revoked, or the access was withdrawn): the same guidance as the check before the job, or nil when err is something else.
func (s *Service) hfDownloadDenied(ctx context.Context, fileURL string, err error) error {
	var he *fetch.HTTPError
	if ctx.Err() != nil || !errors.As(err, &he) || (he.Status != http.StatusUnauthorized && he.Status != http.StatusForbidden) {
		return nil
	}
	repo, _, _, ok := hfFileAt(fileURL)
	if !ok {
		return nil
	}
	return &userError{s.hfDenied(he.Status, "", repo), fmt.Errorf("%w: %w", errHFAccess, err)}
}
