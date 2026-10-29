// Package hfguide is the guidance for a model that is behind a Hugging Face login (catalog.Model.RequiresHFToken, a gated
// repository such as Lightricks/LTX-2.5) and the checks that give it (spec/architecture/model-manager/model-manager.md §5.3). The
// words are defined here once: the screen, the MCP tools, the refusal of modelmanager.Service.InstallModel and the failure of a
// download that Hugging Face refuses half way all tell the user the same thing. It is a package of its own because package
// modelmanager is at its size limit.
package hfguide

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
	"github.com/ousiassllc/mam/backend/internal/service/modelsettings"
)

// TokenSection is where the token is entered on the screen: the card "Hugging Face のトークン" of the model manager.
const TokenSection = "/model-manager#mm-hf"

// NoToken is what a model that needs the token says while none is stored (message A).
const NoToken = "このモデルの取得には Hugging Face のトークンが要ります。モデルマネージャーの「Hugging Face のトークン（任意）」の区画（" +
	TokenSection + "）でトークンを設定してください（MCP では set_huggingface_token）"

// InvalidToken is what Hugging Face's 401 means to the user: the stored token is not accepted (message B).
const InvalidToken = "Hugging Face がトークンを受け付けませんでした（無効か、期限切れです）。Hugging Face でトークンを発行し直し、「Hugging Face のトークン（任意）」の区画（" +
	TokenSection + "）で保存し直してください"

// noAccess is what Hugging Face's 403 (GatedRepo) means: the token is fine but the model's page was not agreed to, or the request for
// access is not approved yet (message C). Access is asked for on the page, never through the API.
func noAccess(repo, page string) string {
	return fmt.Sprintf("このトークンでは %s を取得できません。Hugging Face のモデルのページ（%s）でライセンスに同意してアクセスを申請し、承認されてからやり直してください（mam からは申請できません）", repo, page)
}

// Denied is the message for the refusal of a file of repo: 401 is a token that is not accepted (B), 403 and a GatedRepo answer are an
// access that was not granted (C).
func Denied(status int, code, repo, page string) string {
	if status == http.StatusUnauthorized && code != "GatedRepo" {
		return InvalidToken
	}
	return noAccess(repo, page)
}

func refuse(msg string) error { return &modelsettings.InvalidError{Message: msg} }

// RequireToken is the first check of an install of a model that needs the token: none stored is message A (*InvalidError).
func RequireToken(c *hf.Client) error {
	if !c.HasToken() {
		return refuse(NoToken)
	}
	return nil
}

// CheckAccess is the last check, before the job is made: the first byte of one file of each repository the model reads (as
// hfplan's checkAccess does for an import) shows whether the token is accepted (401, message B) and whether the access was granted
// (403, message C). Those are an *InvalidError, and so are a file that is gone, a rate limit and a failing server (short guidance that does
// not blame the token); any other failure (the network) is returned as it is. Nothing else is
// downloaded.
func CheckAccess(ctx context.Context, c *hf.Client, m catalog.Model) error {
	seen := map[string]bool{}
	for _, f := range m.Files {
		repo, commit, path, ok := fileAt(f.URL)
		if !ok || seen[repo] {
			continue
		}
		seen[repo] = true
		err := c.Probe(ctx, repo, commit, path)
		var ae *hf.APIError
		if errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden) {
			return refuse(Denied(ae.Status, ae.Code, repo, c.PageURL(repo)))
		}
		if msg, told := unreachable(err, repo); told {
			return refuse(msg)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// unreachable is the short guidance for a probe that failed for a reason that is neither the token nor the access: the file is gone
// (404), Hugging Face limits the requests (429) or is failing (5xx). None of them says anything about the token, so none blames it.
// told is false for nil and for any other failure (the network), which the caller words (Service.hfError).
func unreachable(err error, repo string) (msg string, told bool) {
	var ae *hf.APIError
	var rl *hf.RateLimitError
	switch {
	case errors.As(err, &rl):
		return "Hugging Face のレート制限に達しました。しばらく待ってからやり直してください", true
	case errors.As(err, &ae) && ae.Status == http.StatusNotFound:
		return fmt.Sprintf("Hugging Face に %s のファイルが見つかりません（HTTP 404）。取得元が移動したか削除された可能性があります", repo), true
	case errors.As(err, &ae) && ae.Status >= http.StatusInternalServerError:
		return fmt.Sprintf("Hugging Face が一時的に応答できません（HTTP %d）。しばらく待ってからやり直してください", ae.Status), true
	}
	return "", false
}

// DownloadDenied is the message for a download that Hugging Face refused after the job was queued (the token was cleared or revoked,
// or the access was withdrawn): the same guidance as the check before the job, so a token that is no longer stored is message A, not B.
// ok is false when err is something else.
func DownloadDenied(ctx context.Context, c *hf.Client, fileURL string, err error) (msg string, ok bool) {
	var he *fetch.HTTPError
	if ctx.Err() != nil || !errors.As(err, &he) || (he.Status != http.StatusUnauthorized && he.Status != http.StatusForbidden) {
		return "", false
	}
	repo, _, _, found := fileAt(fileURL)
	if !found {
		return "", false
	}
	if !c.HasToken() {
		return NoToken, true
	}
	return Denied(he.Status, "", repo, c.PageURL(repo)), true
}

// fileAt splits the URL of a file of a Hugging Face repository: <base>/<owner>/<name>/resolve/<commit>/<path>.
func fileAt(raw string) (repo, commit, path string, ok bool) {
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
