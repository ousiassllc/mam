// Package mmview holds what the screen of the model manager and its MCP tools share beyond modelmanager.Service: how a
// refusal of the service is told to the user (Refuse) and the models a trial generation can use (TrialModels). It is a
// package of its own because package modelmanager is at its size limit.
package mmview

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// RefusalKind sorts the refusals of the service for the callers that turn them into a status: the screen's HTTP
// status and the MCP tool error.
type RefusalKind int

// The kinds of a refusal. RefusalNone is an error that is not a refusal (a fault: log it, show nothing of it).
const (
	RefusalNone RefusalKind = iota
	// RefusalInvalid: the input cannot be accepted as it is (422).
	RefusalInvalid
	// RefusalConflict: the state does not allow it now (409): a missing consent or engine, a running install, a
	// pinned setting file, an OS without the bundled engine.
	RefusalConflict
	// RefusalNotFound: no such model, job or trial (404).
	RefusalNotFound
)

// Refuse says how an error of the Service is a refusal and the message to show for it (Japanese, for the user).
// An error that is none of them is RefusalNone with no message.
func Refuse(err error) (RefusalKind, string) {
	var invalid *modelmanager.InvalidError
	var noSpace *fetch.NoSpaceError
	switch {
	case err == nil:
		return RefusalNone, ""
	case errors.As(err, &invalid):
		return RefusalInvalid, invalid.Message
	case errors.Is(err, modelmanager.ErrNotFound):
		return RefusalNotFound, "対象が見つかりません。画面を読み込み直してください"
	case errors.Is(err, modelmanager.ErrConsentRequired):
		return RefusalConflict, "このモデルのライセンスに同意してから導入してください"
	case errors.Is(err, modelmanager.ErrBusy):
		return RefusalConflict, "導入または試し生成が実行中です。終わってから、またはキャンセルしてからやり直してください"
	case errors.Is(err, modelmanager.ErrNoGPU):
		return RefusalInvalid, guidance(err)
	case errors.Is(err, modelmanager.ErrPinned):
		return RefusalConflict, "起動オプション（--model-config）でモデルの設定ファイルが固定されているため、導入と削除はできません"
	case errors.Is(err, modelmanager.ErrUnsupported):
		return RefusalConflict, "同梱の ComfyUI は Windows 版だけで使えます。この環境では、設定の「ローカル・代替モデル」に外部の ComfyUI の URL を登録して使ってください"
	case errors.Is(err, modelmanager.ErrEngineNotInstalled):
		return RefusalConflict, "先に ComfyUI エンジンを導入してください"
	case errors.Is(err, modelmanager.ErrAlreadyInstalled):
		return RefusalConflict, "このモデルはすでに導入済みです"
	case errors.Is(err, modelmanager.ErrNotInstalled):
		return RefusalConflict, "このモデルは導入されていません"
	case errors.Is(err, modelmanager.ErrEndpointMoved):
		return RefusalConflict, modelmanager.ErrEndpointMoved.Error()
	case errors.As(err, &noSpace):
		return RefusalConflict, "空き容量が足りません（必要 " + humanBytes(noSpace.Need) + "、空き " + humanBytes(noSpace.Free) + "、場所 " + noSpace.Path + "）。空けてからやり直してください"
	case errors.Is(err, store.ErrNotWritable):
		return RefusalConflict, "mam.exe のあるフォルダに書き込めません。書き込める場所に置くか、外部の ComfyUI の URL を使ってください"
	case errors.Is(err, engine.ErrVariant):
		return RefusalInvalid, "その GPU 向けのビルドは用意していません"
	case errors.Is(err, engine.ErrBusyRunning):
		return RefusalConflict, "ComfyUI が動いています。止めてからやり直してください"
	}
	return RefusalNone, ""
}

// guidance is what follows "<ErrNoGPU>: " (the advice of the GPU detection), or a default.
func guidance(err error) string {
	if msg, ok := strings.CutPrefix(err.Error(), modelmanager.ErrNoGPU.Error()+": "); ok && msg != "" {
		return msg
	}
	return "対応する GPU が見つかりません。外部の ComfyUI の URL を設定するか、CPU 実行を明示的に選んでください"
}

func humanBytes(n uint64) string {
	const gib = 1 << 30
	if n >= gib {
		return fmt.Sprintf("%.1f GiB", float64(n)/gib)
	}
	return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
}
