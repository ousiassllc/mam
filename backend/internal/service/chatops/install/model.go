package install

import (
	"context"
	"errors"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/nlu"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
)

// Model is the Handler of OpInstallModel.
type Model struct{ deps ModelDeps }

// NewModel returns the handler of OpInstallModel.
func NewModel(d ModelDeps) *Model { return &Model{deps: d} }

const whichModel = "どのモデルを導入しますか？「FLUX.1 schnell を入れて」のように、モデルの名前を添えて送ってください。"

// Question resolves what the director said against the catalog (the model's word is only a name: nothing that is not in the catalog is ever installed), and shows what the
// install fetches and under which license before anything starts. Several candidates are a card of buttons, none a refusal.
func (m *Model) Question(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	var a ModelArgs
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	info, candidates, err := m.find(a.Model)
	switch {
	case err != nil:
		return chatops.Card{}, err
	case len(candidates) > 0:
		ops := make([]commandinterpreter.Operation, len(candidates))
		for i, c := range candidates {
			if ops[i], err = commandinterpreter.NewOperation(OpInstallModel, ModelArgs{Model: c.ID, Pick: true}); err != nil {
				return chatops.Card{}, err
			}
		}
		return choiceCard("モデル", labels(candidates), ops), nil
	}
	return m.confirm(ctx, info)
}

// find is the one model the director named, or the several that match it. The catalog the director may see is the one of Catalog (an adult model only after the age
// confirmation); a name that finds nothing there is tried as an id, so that the age gate answers for an adult model as it does for the MCP tool.
func (m *Model) find(query string) (ModelInfo, []named, error) {
	if nlu.Norm(query) == "" {
		return ModelInfo{}, nil, chatops.Refuse("%s", whichModel)
	}
	models := m.deps.Catalog()
	items := make([]named, len(models))
	for i, mi := range models {
		items[i] = named{ID: mi.ID, Name: mi.Name}
	}
	found := resolve(query, items)
	switch {
	case len(found) == 1:
		return m.byID(found[0].ID)
	case len(found) > chatops.MaxChoices:
		return ModelInfo{}, nil, tooMany(query, names(found))
	case len(found) > 1:
		return ModelInfo{}, found, nil
	}
	info, err := m.deps.Model(query)
	switch {
	case errors.Is(err, modelmanager.ErrNotFound):
		return ModelInfo{}, nil, chatops.Refuse("カタログに「%s」というモデルはありません。モデルマネージャーの一覧にある名前で言ってください", query)
	case err != nil:
		return ModelInfo{}, nil, refusal(err)
	}
	return info, nil, nil
}

func (m *Model) byID(id string) (ModelInfo, []named, error) {
	info, err := m.deps.Model(id)
	if err != nil {
		return ModelInfo{}, nil, refusal(err)
	}
	return info, nil, nil
}

// confirm is the card of one model: everything that refuses the install is told before it, so that no card is shown for something that would be refused.
func (m *Model) confirm(ctx context.Context, info ModelInfo) (chatops.Card, error) {
	switch {
	case info.Installed:
		return chatops.Card{}, chatops.ErrNoChange
	case info.InstallJob != "":
		return chatops.Card{}, installing(info.Name)
	case info.NodeReason != "":
		return chatops.Card{}, chatops.Refuse("%s", info.NodeReason)
	}
	env, err := m.deps.Environment(ctx)
	if err != nil {
		return chatops.Card{}, err
	}
	if err := environment(env); err != nil {
		return chatops.Card{}, err
	}
	if info.RequiresHFToken {
		if has, err := m.deps.HasHFToken(); err != nil {
			return chatops.Card{}, err
		} else if !has {
			return chatops.Card{}, chatops.Refuse("%s", hfguide.NoToken)
		}
	}
	if err := room(info.NeedBytes, env.FreeBytes); err != nil {
		return chatops.Card{}, err
	}
	text := "モデル「" + info.Name + "」を導入しますね？\n" + sizeLine(info.TotalBytes, info.NeedBytes, env.FreeBytes) + licenseLines(info.License)
	if info.VRAMMinGB > 0 {
		text += fmt.Sprintf("・VRAM の目安: %d GB 以上\n", info.VRAMMinGB)
	}
	if info.RequiresHFToken {
		text += "・Hugging Face のトークン（設定済み）で取得します。\n"
	}
	if info.AdultNotice != "" {
		text += "・" + info.AdultNotice + "\n"
	}
	card := chatops.Card{}
	if info.ConsentRequired && !info.Accepted {
		return m.consentCard(card, info, text)
	}
	op, err := commandinterpreter.NewOperation(OpInstallModel, ModelArgs{Model: info.ID})
	if err != nil {
		return chatops.Card{}, err
	}
	card.Text, card.Op = text+globalNote, &op
	return card, nil
}

// consentCard asks for the acceptance of the license: the button "同意して導入する" carries the ID and version of the license the card shows, which the install records as the
// user's acceptance. A yes in words does not accept it.
func (m *Model) consentCard(card chatops.Card, info ModelInfo, text string) (chatops.Card, error) {
	l := info.License
	if l.ID == "" || l.Version == "" {
		return chatops.Card{}, chatops.Refuse("「%s」のライセンスの ID・版を確かめられないため、同意を記録できません。モデルマネージャーの画面から導入してください", info.Name)
	}
	op, err := commandinterpreter.NewOperation(OpInstallModel, ModelArgs{Model: info.ID, LicenseID: l.ID, LicenseVersion: l.Version})
	if err != nil {
		return chatops.Card{}, err
	}
	text += "・このモデルは、ライセンスへの同意が要ります。"
	if l.Summary != "" {
		text += "\n　" + l.Summary
	}
	if l.URL != "" {
		text += "\n　原文: " + l.URL
	}
	text += "\n・下の「" + consentLabel + "」を押すと、ライセンス「" + l.ID + "」（版 " + l.Version + "）に同意したことになります。内容を読んでから押してください。\n"
	card.Text, card.Choices = text+globalNote, []chatops.Choice{{Label: consentLabel, Op: op}}
	return card, nil
}

// Apply queues the install, as install_model does. The id is read from the catalog again (a button never installs what is not in it), and the license the card showed is
// the acceptance the install records. A button that offered several models only asks the confirmation of the one picked.
func (m *Model) Apply(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a ModelArgs
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	info, err := m.deps.Model(a.Model)
	if err != nil {
		return chatops.Result{}, refusal(err)
	}
	if a.Pick {
		card, err := m.confirm(ctx, info)
		if errors.Is(err, chatops.ErrNoChange) {
			return chatops.Result{Text: "「" + info.Name + "」はすでに導入済みです。"}, nil
		} else if err != nil {
			return chatops.Result{}, err
		}
		pick, err := commandinterpreter.NewOperation(OpInstallModel, ModelArgs{Model: info.ID})
		if err != nil {
			return chatops.Result{}, err
		}
		msg, err := chatops.Message(pick, card)
		return chatops.Result{Text: "「" + info.Name + "」ですね。導入の前に確かめてください。", Messages: []domain.ChatMessage{msg}}, err
	}
	var accept *LicenseRef
	if a.LicenseID != "" || a.LicenseVersion != "" {
		accept = &LicenseRef{ID: a.LicenseID, Version: a.LicenseVersion}
	}
	job, err := m.deps.Install(ctx, info.ID, accept)
	if err != nil {
		return chatops.Result{}, applyRefusal(err)
	}
	return started(job, info.Name), nil
}

// applyRefusal tells the error of an install as the MCP tool does; a refusal that is already one stays.
func applyRefusal(err error) error {
	if _, ok := asRefusal(err); ok {
		return err
	}
	return refusal(err)
}
