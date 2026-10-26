package settings

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/appupdate"
)

// UpdateSection is the "バージョンとアップデート" section.
type UpdateSection struct {
	Available bool      `json:"available" jsonschema:"false はアップデートできない版（ヘッドレス）。version だけを示す"`
	Version   string    `json:"version" jsonschema:"今のバージョン。dev は作業ツリーからのビルド"`
	State     string    `json:"state,omitempty" jsonschema:"checking, error, ready, blocked, available, dev_build, no_release, up_to_date or unchecked"`
	CheckedAt time.Time `json:"checked_at,omitzero" jsonschema:"最後の確認が終わった時刻。まだなら省略"`
	Latest    string    `json:"latest,omitempty" jsonschema:"新しいリリースのタグ"`
	BlockedBy string    `json:"blocked_by,omitempty" jsonschema:"state が blocked のとき、更新を待たせている作業（jobs, editing, media, check_failed）"`
	ErrorKind string    `json:"error_kind,omitempty" jsonschema:"state が error のときの種類（network, rate_limit, access, verification, release, other）"`
}

func updateSection(r *mcpkit.Registrar) UpdateSection {
	svc := r.Settings.Update
	if svc == nil {
		return UpdateSection{Version: r.Version}
	}
	return updateOf(svc.View())
}

func updateOf(v appupdate.View) UpdateSection {
	return UpdateSection{
		Available: true, Version: v.Version, State: string(v.State), CheckedAt: v.CheckedAt, Latest: v.Latest,
		BlockedBy: string(v.BlockedBy), ErrorKind: string(v.ErrorKind),
	}
}

func (h handlers) updateStatus(_ context.Context, _ Input) (UpdateSection, error) {
	if h.r.Settings.Update == nil {
		return UpdateSection{}, unavailable("アップデートの確認")
	}
	return updateSection(h.r), nil
}

// checkUpdate runs a check as POST /settings/update/check does. The failure of the check is the state of
// the answer (state "error"), not a tool error; a verified newer release makes the app quit, so this answer
// may never arrive.
func (h handlers) checkUpdate(ctx context.Context, _ Input) (UpdateSection, error) {
	svc := h.r.Settings.Update
	if svc == nil {
		return UpdateSection{}, unavailable("アップデートの確認")
	}
	v, _ := svc.CheckNow(ctx) // CheckNow's service has logged a failure; the view shows it
	return updateOf(v), nil
}
