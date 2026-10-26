package generation

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// bytesPerVideoSecond is the assumed size of one second of generated video (about 16 Mbit/s: the
// 1080p clips of the models). It is only used to warn before the disk fills up.
const bytesPerVideoSecond = 2 << 20

// warnLowDisk tells the user in the thread when the free space of the assets volume is smaller than
// what the Takes of the strategy are expected to take. Generation still starts: the estimate is rough
// (non-functional.md §4 "警告してから生成を開始する").
func (o *Orchestrator) warnLowDisk(ctx context.Context, ref repository.ShotRef, s Strategy) {
	need := uint64(float64(s.Takes) * s.DurationSeconds * bytesPerVideoSecond)
	free, err := o.freeDisk(o.files.root)
	if err != nil {
		o.logger.Warn("read free disk space failed", "err", err)
		return
	}
	if free >= need {
		return
	}
	notice := domain.ChatMessage{
		Role: domain.RoleAssistant, Kind: domain.KindText, RefType: domain.RefSettings,
		Content: fmt.Sprintf("ディスクの空き容量が少ない可能性があります（Shot %d の生成に約 %s 必要な見込みに対し、空きは %s）。"+
			"不要な Take は設定画面からアーカイブできます。", ref.Shot.OrderIndex+1, domain.FormatBytes(need), domain.FormatBytes(free)),
	}
	if err := o.chat.Announce(ctx, ref.ProjectID, notice); err != nil {
		o.logger.Error("post disk warning failed", "shot_id", ref.Shot.ID, "err", err)
	}
}
