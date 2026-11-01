package audiodirector

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

// lipSyncer is who redoes the mouths of one run, the model its clips are recorded under in the ledger and on the clip (and the
// request's name for it), and what a clip costs (the local lip sync costs nothing to call, and is still recorded).
type lipSyncer struct {
	model domain.ModelName
	name  string
	price func(seconds float64) float64
	sync  LipSyncer
}

// lipSyncerFor chooses who redoes the mouths of the run (Sync Labs unless the run asks for the local one). ok is false when none can
// be used: the reason is then in Skipped (the local runtime that is not installed, whose weights are missing or that cannot run on
// this machine is known here, once, and the clips are not tried one by one), and the run goes on without Lip Sync.
func (r *run) lipSyncerFor(ctx context.Context) (ls lipSyncer, ok bool, err error) {
	if r.in.LipSyncProvider != localvoice.LipSyncLocal {
		if r.d.Providers.LipSync == nil {
			r.skip(domain.LayerLipSync, "", domain.SkipError, "Lip Sync のプロバイダーが設定されていません")
			return lipSyncer{}, false, nil
		}
		model := r.scope.LipSyncModel
		return lipSyncer{domain.ModelSyncLabs, model, func(seconds float64) float64 { return r.d.Prices.LipSync(model, seconds) }, r.d.Providers.LipSync}, true, nil
	}
	local := r.d.Providers.LocalLipSync
	if local == nil {
		r.skip(domain.LayerLipSync, "", domain.SkipError, "ローカルの口パクのプロバイダーが設定されていません")
		return lipSyncer{}, false, nil
	}
	if err := local.Prepare(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return lipSyncer{}, false, ctxErr
		}
		r.skip(domain.LayerLipSync, "", domain.SkipError, localvoice.LipSyncReason(err))
		return lipSyncer{}, false, nil
	}
	return lipSyncer{domain.ModelMuseTalk, "", func(float64) float64 { return 0 }, local}, true, nil
}
