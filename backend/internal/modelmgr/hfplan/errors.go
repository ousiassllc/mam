package hfplan

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
)

// ErrAdult means the repository carries the not-for-all-audiences tag while the age gate is closed: nothing of it is
// shown and nothing is fetched.
var ErrAdult = errors.New("hfplan: the repository is for adults and the age confirmation is missing")

// Refusal is a refusal the user can act on; Message (Japanese) says what and what to do next.
type Refusal struct{ Message string }

func (r *Refusal) Error() string { return r.Message }

func refusal(format string, args ...any) error {
	return &Refusal{Message: fmt.Sprintf(format, args...)}
}

// explain turns a failure of a Hugging Face read into a Refusal where the user can do something about it (a wrong
// input, a missing repository, a rate limit); any other error (the network, a bad answer) is returned as it is.
func explain(err error, repo string, hasToken bool) error {
	var api *hf.APIError
	var rate *hf.RateLimitError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, hf.ErrInput):
		return &Refusal{Message: strings.TrimPrefix(err.Error(), hf.ErrInput.Error()+": ")}
	case errors.As(err, &rate):
		if hasToken {
			return refusal("Hugging Face のレート制限に達しました。しばらく待ってからやり直してください")
		}
		return refusal("Hugging Face のレート制限に達しました（トークンなしの利用は制限が厳しいです）。しばらく待つか、Hugging Face のトークンを設定してからやり直してください")
	case errors.As(err, &api) && (api.Status == 404 || api.Status == 401 || api.Status == 403):
		if hasToken {
			return refusal("%s を読めません。リポジトリが存在しないか、非公開で、設定したトークンに権限がありません", repo)
		}
		return refusal("%s を読めません。リポジトリが存在しないか、非公開です（非公開のリポジトリはトークンの設定が要ります）", repo)
	}
	return err
}
