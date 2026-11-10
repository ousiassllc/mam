package install

import (
	"context"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/engine"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

// Engine is the Handler of OpInstallEngine.
type Engine struct{ deps EngineDeps }

// NewEngine returns the handler of OpInstallEngine.
func NewEngine(d EngineDeps) *Engine { return &Engine{deps: d} }

const engineName = "ComfyUI エンジン"

// Question chooses the build as install_model_engine does (the one of the GPU, or the CPU when asked for) and shows what it downloads and unpacks. An engine that is up to
// date is nothing to do; one that is out of date is updated by installing again.
func (e *Engine) Question(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	var a EngineArgs
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	info, err := e.deps.Engine(ctx)
	if err != nil {
		return chatops.Card{}, err
	}
	switch {
	case !info.Supported:
		return chatops.Card{}, refusal(modelmanager.ErrUnsupported)
	case info.Pinned:
		return chatops.Card{}, refusal(modelmanager.ErrPinned)
	case !info.Writable:
		return chatops.Card{}, refusal(store.ErrNotWritable)
	case info.InstallJob != "":
		return chatops.Card{}, installing(engineName)
	case info.Installed && !info.Broken && !info.UpdateAvailable:
		return chatops.Card{}, chatops.ErrNoChange
	}
	variant, err := chosen(info, a)
	if err != nil {
		return chatops.Card{}, err
	}
	if err := room(variant.NeedBytes, info.FreeBytes); err != nil {
		return chatops.Card{}, err
	}
	pick, err := commandinterpreter.NewOperation(OpInstallEngine, EngineArgs{Variant: variant.ID, CPU: a.CPU})
	if err != nil {
		return chatops.Card{}, err
	}
	return chatops.Card{Text: engineText(info, variant, a.CPU), Op: &pick}, nil
}

// chosen is the build the install uses: the one the director named when it is one of the machine's, the GPU's recommendation, or the first build when the CPU is asked
// for (the service takes the same one). It is a refusal when the machine has no supported GPU and the CPU was not asked for, or the build is none of the machine's.
func chosen(info EngineInfo, a EngineArgs) (Variant, error) {
	want := a.Variant
	switch {
	case a.CPU && len(info.Variants) > 0:
		return info.Variants[0], nil
	case want == "":
		want = info.RecommendedVariant
	}
	if want == "" {
		return Variant{}, refusal(fmt.Errorf("%w: %s", modelmanager.ErrNoGPU, info.RecommendationUnavailable))
	}
	if i := slices.IndexFunc(info.Variants, func(v Variant) bool { return v.ID == want }); i >= 0 {
		return info.Variants[i], nil
	}
	return Variant{}, refusal(engine.ErrVariant)
}

func engineText(info EngineInfo, v Variant, cpu bool) string {
	text := "ComfyUI エンジン（ローカルの画像・動画生成の土台）を導入しますね？\n"
	switch {
	case info.Installed && info.UpdateAvailable:
		text = "ComfyUI エンジンを更新しますね？（導入済み " + info.Version + " → " + info.PinnedVersion + "）\n"
	case info.Broken:
		text = "ComfyUI エンジンを入れ直しますね？（導入の記録はありますが、ファイルがありません）\n"
	}
	text += "・ビルド: " + v.Label
	if cpu {
		text += "（GPU を使わず CPU で動かします。とても遅く、動画には向きません）"
	}
	text += "\n・ダウンロード: 約 " + bytesText(v.Size) + "、展開後 約 " + bytesText(v.ExtractedSize) + "（必要な空き容量 " + bytesText(v.NeedBytes) + "）\n"
	if info.FreeBytes > 0 {
		text += "・導入先の空き容量: " + bytesText(signed(info.FreeBytes)) + "\n"
	}
	if info.LicenseName != "" {
		text += "・ライセンス: " + info.LicenseName + "（ComfyUI 本体。導入するのは配布元のポータブル版です）\n"
	}
	return text + "・チェックサム（SHA-256）を確かめ、中断しても続きから再開できます。\n" + globalNote
}

// Apply queues the install as install_model_engine does.
func (e *Engine) Apply(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a EngineArgs
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	job, err := e.deps.Install(ctx, a.Variant, a.CPU)
	if err != nil {
		return chatops.Result{}, applyRefusal(err)
	}
	return started(job, engineName), nil
}
