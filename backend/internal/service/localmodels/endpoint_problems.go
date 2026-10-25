package localmodels

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
)

// endpointError is the *InvalidError of the problems localcfg.ValidateEndpoint found: one Japanese line per problem, in the
// order of the fields, for a screen (the English lines are for the settings file's check and the log). nil for no problem.
func endpointError(errs []error) error {
	var lines []string
	for _, err := range errs {
		lines = append(lines, endpointProblem(err))
	}
	if len(lines) == 0 {
		return nil
	}
	return &InvalidError{Message: strings.Join(lines, "\n")}
}

func endpointProblem(err error) string {
	var p *localcfg.EndpointError
	if !errors.As(err, &p) {
		return err.Error()
	}
	switch p.Field {
	case localcfg.FieldName:
		return "名前は 1〜63 文字で、英小文字・数字・「.」「_」「-」だけにしてください（先頭は英小文字か数字）。"
	case localcfg.FieldKind:
		return "種類は ComfyUI か OpenAI 互換のどちらかを選んでください。"
	case localcfg.FieldBaseURL:
		return baseURLProblem(p.Reason)
	case localcfg.FieldMaxConcurrency:
		return fmt.Sprintf("同時実行数は 1〜%d の整数で入力してください（空: 1）。", localcfg.MaxConcurrency)
	case localcfg.FieldTimeoutSeconds:
		return fmt.Sprintf("タイムアウトは 1〜%d 秒で入力してください（空: ComfyUI 3600・互換 900）。", localcfg.MaxTimeoutSeconds)
	}
	return err.Error()
}

func baseURLProblem(reason error) string {
	switch {
	case errors.Is(reason, localcfg.ErrBaseURLScheme):
		return "接続先の URL は http:// か https:// で始めてください。"
	case errors.Is(reason, localcfg.ErrBaseURLParts):
		return "接続先の URL に、ユーザー名・クエリ（? 以降）・フラグメント（# 以降）は付けられません。"
	case errors.Is(reason, localcfg.ErrBaseURLLinkLocal):
		return "接続先の URL に、リンクローカル（169.254.x.x・fe80::）、未指定、マルチキャストのアドレスは使えません。" +
			"登録した接続先でも使えません。クラウドのメタデータサービスなどに届いてしまうためです。"
	}
	return "接続先の URL は http://127.0.0.1:8188 のような形で入力してください。"
}
