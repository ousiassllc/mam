package settings

import (
	"context"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/apprestart"
)

// RestartInput is the request to restart the app. Nothing happens unless Confirm is true.
type RestartInput struct {
	Confirm     bool   `json:"confirm" jsonschema:"true を明示したときだけ再起動する。必須。false は何もせず validation になる（省略はスキーマで拒否される）"`
	WhenBusy    string `json:"when_busy,omitempty" jsonschema:"動いている作業（生成などのジョブ・編集・ffmpeg・ワークフロー実行・ダウンロード）があるときの扱い。refuse（既定: 再起動せず conflict で何が動いているか返す）/ wait（終わるまで待ってから再起動。応答は state=waiting）/ stop（中断して今すぐ再起動。生成ジョブは失敗になる。モデル・エンジン・ffmpeg のダウンロードが動いていれば stop でも拒否する）"`
	WaitMinutes int    `json:"wait_minutes,omitempty" jsonschema:"待つ上限（分。when_busy が wait のとき）。既定 30、最大 1440。過ぎたら再起動せずにやめる"`
}

// BusyWork is one thing that was running when the restart was asked for.
type BusyWork struct {
	Kind    string `json:"kind" jsonschema:"jobs / editing / media / workflow_run / download / check_failed"`
	Message string `json:"message"`
}

// RestartOutput is the answer of restart_app. For state=restarting the app ends moments after this answer, so it may never
// arrive; check that it came back with get_health (boot_id is another one).
type RestartOutput struct {
	State     string     `json:"state" jsonschema:"restarting（アプリを終了して起動し直す）/ waiting（作業が終わったら再起動する）"`
	BootID    string     `json:"boot_id" jsonschema:"再起動を頼んだ時点の実行の ID。再起動後の get_health の boot_id がこれと違えば、再起動できている"`
	Busy      []BusyWork `json:"busy" jsonschema:"頼んだ時点で動いていた作業（stop で中断するもの、wait で待つもの）。無ければ空"`
	WaitUntil string     `json:"wait_until,omitempty" jsonschema:"待っているとき（state が waiting）の、待つのをやめる時刻（RFC 3339）"`
	Next      string     `json:"next"`
}

func (h handlers) restart(ctx context.Context, in RestartInput) (RestartOutput, error) {
	svc := h.r.Settings.Restart
	if svc == nil || !svc.Available() {
		return RestartOutput{}, Unavailable("アプリの再起動")
	}
	if !in.Confirm {
		return RestartOutput{}, invalidInput("confirm", "再起動するには confirm: true を指定してください（確認なしでは再起動しません）")
	}
	when := apprestart.WhenBusy(in.WhenBusy)
	if !when.Valid() {
		return RestartOutput{}, invalidInput("when_busy", "refuse・wait・stop のどれかを指定してください")
	}
	if in.WaitMinutes < 0 {
		return RestartOutput{}, invalidInput("wait_minutes", "0 以上の分で指定してください")
	}
	res, err := svc.Request(ctx, apprestart.Options{WhenBusy: when, WaitFor: time.Duration(in.WaitMinutes) * time.Minute})
	var busy *apprestart.BusyError
	switch {
	case errors.As(err, &busy):
		return RestartOutput{}, &service.ConflictError{Message: busy.Error()}
	case errors.Is(err, apprestart.ErrUnavailable):
		return RestartOutput{}, Unavailable("アプリの再起動")
	case err != nil:
		return RestartOutput{}, err
	}
	out := RestartOutput{State: string(res.State), BootID: res.BootID, Busy: make([]BusyWork, 0, len(res.Busy))}
	for _, b := range res.Busy {
		out.Busy = append(out.Busy, BusyWork{Kind: b.Kind, Message: b.Message})
	}
	if res.State == apprestart.StateWaiting {
		out.WaitUntil = res.WaitUntil.UTC().Format(time.RFC3339)
		out.Next = "作業が終わると再起動します。get_health の restart_waiting で待っているかが、boot_id が変われば再起動できたかが分かります"
		return out, nil
	}
	out.Next = "アプリはまもなく終了して起動し直します（MCP の接続は数秒切れます）。get_health の boot_id が変わり ok=true になれば再起動できています"
	return out, nil
}
