package mmview

import (
	"errors"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// RefuseRuntime is Refuse for the errors of the local runtimes (localruntime.Service): the same kinds, so the screen answers 409 /
// 404 and the MCP tools conflict / not_found the same way. A refusal because of the machine (*localruntime.SupportError) carries its
// own sentence (the reason and the cloud provider to use instead); one that Refuse knows (no disk space, a place that cannot be
// written) is told as Refuse tells it. An error that is none of them is RefusalNone.
func RefuseRuntime(err error) (RefusalKind, string) {
	switch {
	case err == nil:
		return RefusalNone, ""
	case errors.Is(err, localruntime.ErrNotFound):
		return RefusalNotFound, "対象が見つかりません。画面を読み込み直してください"
	case errors.Is(err, localruntime.ErrUnsupported), errors.Is(err, localruntime.ErrWeightsMissing), errors.Is(err, localruntime.ErrSourceGone):
		return RefusalConflict, err.Error()
	case errors.Is(err, localruntime.ErrNotInstalled):
		return RefusalConflict, "導入されていません。先に導入してください"
	case errors.Is(err, localruntime.ErrBusy):
		return RefusalConflict, "導入が実行中です。終わってから、または中断してからやり直してください"
	case errors.Is(err, store.ErrNotWritable):
		return RefusalConflict, "mam.exe のあるフォルダに書き込めません。書き込める場所に置いてください"
	}
	return Refuse(err)
}
