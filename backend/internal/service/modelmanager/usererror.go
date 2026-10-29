package modelmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// userFacing turns a failure into the message the screen shows. A cancelled context stays as it is: the queue
// decides what an interruption means.
func userFacing(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	var ns *fetch.NoSpaceError
	var he *fetch.HTTPError
	switch {
	case errors.As(err, &ns):
		return &userError{fmt.Sprintf("空き容量が足りません（必要 %s、空き %s、場所 %s）", humanBytes(ns.Need), humanBytes(ns.Free), ns.Path), err}
	case errors.Is(err, fetch.ErrChecksum):
		return fmt.Errorf("ダウンロードしたファイルの SHA-256 が一致しません。ダウンロードを破棄しました（取得元の改ざんか転送の破損の可能性があります）: %w", err)
	case errors.Is(err, fetch.ErrSize):
		return fmt.Errorf("ダウンロードしたファイルの大きさが固定した値と違います。ダウンロードを破棄しました: %w", err)
	case errors.As(err, &he) && (he.Status == 401 || he.Status == 403) && !errors.Is(err, errHFAccess): // errHFAccess is already worded by hfguide
		return fmt.Errorf("取得元が認証を求めています（%d）。Hugging Face のトークンを設定してください: %w", he.Status, err)
	case errors.Is(err, ErrAdultNotConfirmed):
		return &userError{AdultGateMessage, err}
	case errors.Is(err, store.ErrNotWritable):
		return fmt.Errorf("mam.exe のあるフォルダに書き込めません。書き込める場所に置くか、外部の ComfyUI の URL を使ってください: %w", err)
	}
	return err
}

// AdultGateMessage is what a refusal of an adult model without the age confirmation tells the user.
const AdultGateMessage = "成人向けモデルです。年齢の確認と注意書きへの同意を済ませ、「成人向けモデルを表示する」をオンにしてください"

// ErrSourceGone means a file of a model answered 404 or 410 at its pinned URL: the repository or the file was deleted. It is the
// failure of a mirror of a third party that went away, which is the case the catalog expects (File.SourceNote).
var ErrSourceGone = errors.New("modelmanager: the source of the file is gone")

// sourceGone is the failure of f when its pinned URL no longer exists. The message names the file and, for a mirror, where
// its original lies (the SourceNote), so the user knows the file was taken down rather than that mam is broken.
func sourceGone(f catalog.File, status int, err error) error {
	msg := fmt.Sprintf("取得元が消えています（%s、HTTP %d）。ミラーまたはリポジトリが削除されたため、このモデルは今は導入できません。", f.Name, status)
	if f.SourceNote != "" {
		msg += " " + f.SourceNote
	}
	return &userError{msg, fmt.Errorf("%w: %s: %w", ErrSourceGone, f.Name, err)}
}

// userError carries the message for the user and the error behind it, which errors.Is / As still reach.
type userError struct {
	msg string
	err error
}

func (e *userError) Error() string { return e.msg }
func (e *userError) Unwrap() error { return e.err }

func humanBytes(n uint64) string {
	const gb = 1 << 30
	if n >= gb {
		return fmt.Sprintf("%.1f GiB", float64(n)/gb)
	}
	return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
}
