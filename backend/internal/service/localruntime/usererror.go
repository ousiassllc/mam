package localruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
)

// userFacing turns a failure of an install into the message the screen shows. A cancelled context stays as it is: the queue decides
// what an interruption means.
func userFacing(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	var ns *fetch.NoSpaceError
	var he *fetch.HTTPError
	switch {
	case errors.As(err, &ns):
		return fmt.Errorf("空き容量が足りません（必要 %s、空き %s、場所 %s）: %w", HumanBytes(ns.Need), HumanBytes(ns.Free), ns.Path, err)
	case errors.Is(err, fetch.ErrChecksum):
		return fmt.Errorf("ダウンロードしたファイルの SHA-256 が一致しません。ダウンロードを破棄しました（取得元の改ざんか転送の破損の可能性があります）: %w", err)
	case errors.Is(err, fetch.ErrSize):
		return fmt.Errorf("ダウンロードしたファイルの大きさが固定した値と違います。ダウンロードを破棄しました: %w", err)
	case errors.As(err, &he) && he.Definitive():
		return fmt.Errorf("取得元が応答しません（HTTP %d）。取得元のファイルが移動または削除された可能性があります: %w", he.Status, err)
	case errors.Is(err, store.ErrNotWritable):
		return fmt.Errorf("mam.exe のあるフォルダに書き込めません。書き込める場所に置いてください: %w", err)
	}
	return err
}

// HumanBytes writes a size for the user (GiB, or MiB below one).
func HumanBytes(n uint64) string {
	const gib = 1 << 30
	if n >= gib {
		return fmt.Sprintf("%.1f GiB", float64(n)/gib)
	}
	return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
}
