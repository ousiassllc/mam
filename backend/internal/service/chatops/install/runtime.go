package install

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
)

// Runtime is the Handler of OpInstallRuntime.
type Runtime struct{ deps RuntimeDeps }

// NewRuntime returns the handler of OpInstallRuntime.
func NewRuntime(d RuntimeDeps) *Runtime { return &Runtime{deps: d} }

const whichRuntime = "どの実行環境を導入しますか？「GPT-SoVITS を入れて」「MuseTalk を導入して」のように、名前を添えて送ってください。"

// Question resolves the runtime against the catalog of the local runtimes and shows what it downloads and unpacks. A machine that cannot run it, a directory that cannot be
// written and a disk without the room are refusals, as for install_local_runtime.
func (r *Runtime) Question(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	var a RuntimeArgs
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	if nlu.Norm(a.Runtime) == "" {
		return chatops.Card{}, chatops.Refuse("%s", whichRuntime)
	}
	list := r.deps.Runtimes(ctx)
	items := make([]named, len(list))
	for i, rt := range list {
		items[i] = named{ID: rt.ID, Name: rt.Name}
	}
	found := resolve(a.Runtime, items)
	switch {
	case len(found) == 0:
		all := make([]string, len(items))
		for i, it := range items {
			all[i] = it.Name
		}
		return chatops.Card{}, chatops.Refuse("「%s」という実行環境はありません。導入できるのは %s です", a.Runtime, strings.Join(all, "、"))
	case len(found) > 1:
		ops := make([]commandinterpreter.Operation, len(found))
		for i, f := range found {
			var err error
			if ops[i], err = commandinterpreter.NewOperation(OpInstallRuntime, RuntimeArgs{Runtime: f.ID, Pick: true}); err != nil {
				return chatops.Card{}, err
			}
		}
		return choiceCard("実行環境", labels(found), ops), nil
	}
	return r.confirm(list[indexOf(items, found[0].ID)])
}

func indexOf(items []named, id string) int {
	for i, it := range items {
		if it.ID == id {
			return i
		}
	}
	return 0
}

func (r *Runtime) confirm(info RuntimeInfo) (chatops.Card, error) {
	switch {
	case info.InstallJob != "":
		return chatops.Card{}, installing(info.Name)
	case info.Unsupported != "":
		return chatops.Card{}, chatops.Refuse("%s", info.Unsupported)
	case info.NotWritable != "":
		return chatops.Card{}, refusal(store.ErrNotWritable)
	case info.Installed && !info.Broken && !info.Updates:
		return chatops.Card{}, chatops.ErrNoChange
	}
	if err := room(info.Variant.NeedBytes, info.FreeBytes); err != nil {
		return chatops.Card{}, err
	}
	text := "ローカルの実行環境「" + info.Name + "」を導入しますね？\n"
	switch {
	case info.Installed && info.Updates:
		text = "ローカルの実行環境「" + info.Name + "」を更新しますね？（導入済み " + info.Version + " → " + info.PinnedVersion + "）\n"
	case info.Broken:
		text = "ローカルの実行環境「" + info.Name + "」を入れ直しますね？（導入の記録はありますが、ファイルがありません）\n"
	}
	v := info.Variant
	text += "・取得するのは Python 環境・本体・重みです。ダウンロード 約 " + bytesText(v.Size) + "、展開後 約 " + bytesText(v.ExtractedSize) + "（必要な空き容量 " + bytesText(v.NeedBytes) + "）\n"
	if info.WeightsBytes > 0 {
		text += "・うち重み・依存ファイルは " + bytesText(info.WeightsBytes) + "。"
		if info.WeightsUnclear > 0 {
			text += "商用利用が「商用OK」と確認できていないものが " + strconv.Itoa(info.WeightsUnclear) + " 件あります（導入後、モデルマネージャーの実行環境の欄で、出典とライセンスを確かめてください）。"
		}
		text += "\n"
	}
	if info.FreeBytes > 0 {
		text += "・導入先の空き容量: " + bytesText(signed(info.FreeBytes)) + "\n"
	}
	if info.License.Name != "" {
		text += licenseLines(info.License)
	}
	card, err := yes(OpInstallRuntime, RuntimeArgs{Runtime: info.ID}, text+"・チェックサム（SHA-256）を確かめ、中断しても続きから再開できます。\n"+globalNote)
	return card, err
}

// Apply queues the install as install_local_runtime does. The runtime is looked up again by its id (the button carries the id the card resolved): a button that offered
// several runtimes only asks the confirmation of the one picked.
func (r *Runtime) Apply(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a RuntimeArgs
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	var info *RuntimeInfo
	for _, rt := range r.deps.Runtimes(ctx) {
		if strings.EqualFold(rt.ID, strings.TrimSpace(a.Runtime)) {
			info = &rt
			break
		}
	}
	if info == nil {
		return chatops.Result{}, chatops.Refuse("「%s」という実行環境はありません", a.Runtime)
	}
	if a.Pick {
		card, err := r.confirm(*info)
		if errors.Is(err, chatops.ErrNoChange) {
			return chatops.Result{Text: "「" + info.Name + "」はすでに導入済みです。"}, nil
		} else if err != nil {
			return chatops.Result{}, err
		}
		pick, err := commandinterpreter.NewOperation(OpInstallRuntime, RuntimeArgs{Runtime: info.ID})
		if err != nil {
			return chatops.Result{}, err
		}
		msg, err := chatops.Message(pick, card)
		return chatops.Result{Text: "「" + info.Name + "」ですね。導入の前に確かめてください。", Messages: []domain.ChatMessage{msg}}, err
	}
	job, err := r.deps.Install(ctx, info.ID)
	if err != nil {
		if _, ok := asRefusal(err); ok {
			return chatops.Result{}, err
		}
		return chatops.Result{}, runtimeRefusal(err)
	}
	return started(job, info.Name), nil
}
