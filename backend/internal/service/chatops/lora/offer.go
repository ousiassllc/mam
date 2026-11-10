package lora

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lorafile"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
)

// Importer is what the operation needs of the model manager's LoRA importer (modelmanager.Service.LoRAs): the same steps the model manager screen takes. A reason the
// user can act on (not a LoRA, too big, no room, too many waiting, a name that is taken) is returned as *chatops.Refusal; any other error is a fault.
type Importer interface {
	Stage(ctx context.Context, name string, declared int64, r io.Reader) (loraimport.Staged, error)
	Commit(ctx context.Context, in loraimport.Input) (loraimport.Model, error)
	Discard(token string)
	Staged(token string) (loraimport.Staged, bool)
}

// gone is what the chat says when the file of a card no longer waits (an hour passed, or it was added already).
const gone = "確認待ちのファイルが見つかりません。もう一度添付してください"

// Offer is the chatops.FileOffer of the .safetensors files of a message, and the chatops.Handler of add_lora and discard_lora, the actions of its card.
type Offer struct{ imp Importer }

// New returns the offer that works through imp.
func New(imp Importer) *Offer { return &Offer{imp: imp} }

// Accepts takes the files with the extension .safetensors, in any case; what is in the file is checked by Offer.
func (o *Offer) Accepts(name string) bool {
	return strings.EqualFold(path.Ext(strings.ReplaceAll(name, `\`, "/")), lorafile.Ext)
}

// Offer stages each file and answers with the card that asks before it is added. A file that is refused (or is there already) has the sentence that says why instead of
// a card; when every file is refused the answer is that refusal, so that the chat tells it as it does for any file it cannot take.
func (o *Offer) Offer(ctx context.Context, _ domain.Project, _ string, files []chatops.File) ([]domain.ChatMessage, error) {
	var (
		cards   []domain.ChatMessage
		tokens  []string
		refused []refusal
	)
	for _, f := range files {
		st, err := o.stage(ctx, f)
		var ref *chatops.Refusal
		if errors.As(err, &ref) {
			refused = append(refused, refusal{file: f.Name, reason: ref.Message})
			continue
		}
		if err == nil {
			var card domain.ChatMessage
			if card, err = o.card(st); err == nil {
				tokens, cards = append(tokens, st.Token), append(cards, card)
				continue
			}
		}
		for _, token := range tokens { // a fault: nothing of this message is kept
			o.imp.Discard(token)
		}
		return nil, err
	}
	if len(cards) == 0 {
		return nil, chatops.Refuse("%s", refusedText(refused, len(files) > 1))
	}
	for _, r := range refused {
		cards = append(cards, text("取り込めませんでした: "+r.text(true)))
	}
	return cards, nil
}

// stage checks the file the way the model manager screen does. A file that is there already is not asked: it is thrown away and told.
func (o *Offer) stage(ctx context.Context, f chatops.File) (loraimport.Staged, error) {
	r, err := f.Open()
	if err != nil {
		return loraimport.Staged{}, fmt.Errorf("open the attached file %q: %w", f.Name, err)
	}
	defer func() { _ = r.Close() }()
	st, err := o.imp.Stage(ctx, f.Name, f.Size, r)
	if err != nil {
		return loraimport.Staged{}, err
	}
	if c := st.Conflict; c != nil && c.Same {
		o.imp.Discard(st.Token)
		return loraimport.Staged{}, chatops.Refuse("同じ内容のファイルが、すでに「%s」としてモデルの管理にあります。追加は要りません", c.Name)
	}
	return st, nil
}

// card is the confirmation card of a staged file. Its yes carries add_lora and "やめる" discard_lora; its ref_id names the file, so that the cards of several files
// (or of two messages) can each be answered: the chat only answers the newest card of one ref_id.
func (o *Offer) card(st loraimport.Staged) (domain.ChatMessage, error) {
	name, add, err := o.addOp(st)
	if err != nil {
		return domain.ChatMessage{}, err
	}
	discard, err := commandinterpreter.NewOperation(OpDiscard, Args{Token: st.Token})
	if err != nil {
		return domain.ChatMessage{}, err
	}
	msg, err := chatops.Message(add, chatops.Card{Text: cardText(st, name), Decline: &discard})
	if err != nil {
		return domain.ChatMessage{}, err
	}
	msg.RefID = string(OpAdd) + ":" + st.Token
	return msg, nil
}

// addOp is the operation the yes button carries, and the name the file is added under: when the name it would get is taken by another file, the free name the model
// manager screen suggests (its form is filled with it) and never the name of the other file, which is not overwritten.
func (o *Offer) addOp(st loraimport.Staged) (string, commandinterpreter.Operation, error) {
	args := Args{Token: st.Token}
	name := st.Name
	if c := st.Conflict; c != nil && c.Suggest != "" {
		args.Name, name = c.Suggest, c.Suggest
	}
	op, err := commandinterpreter.NewOperation(OpAdd, args)
	return name, op, err
}

// Question is the card of a file that waits. The card is posted by Offer; this builds it again for a request that names the token.
func (o *Offer) Question(_ context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	if op.Type != OpAdd {
		return chatops.Card{}, chatops.Refuse("この操作は、確認のカードから実行されます")
	}
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	st, ok := o.imp.Staged(a.Token)
	if !ok {
		return chatops.Card{}, chatops.Refuse(gone)
	}
	name, _, err := o.addOp(st)
	if err != nil {
		return chatops.Card{}, err
	}
	discard, err := commandinterpreter.NewOperation(OpDiscard, Args{Token: st.Token})
	if err != nil {
		return chatops.Card{}, err
	}
	return chatops.Card{Text: cardText(st, name), Decline: &discard}, nil
}

// Apply adds the staged file (add_lora: the yes of the card) or throws it away (discard_lora: declining it).
func (o *Offer) Apply(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	if a.Token == "" {
		return chatops.Result{}, chatops.Refuse(gone)
	}
	if op.Type == OpDiscard {
		o.imp.Discard(a.Token)
		return chatops.Result{Text: "取り込みをやめました。ファイルは破棄し、何も追加していません。"}, nil
	}
	return o.add(ctx, a)
}

// add places the staged file in the loras folder with what the file says of itself: the base models, trigger words and license name the model manager form is filled
// with, and nothing more (the strength and the answer to commercial use stay 未設定 and 不明). A refusal discards the file: only this card could reach it.
func (o *Offer) add(ctx context.Context, a Args) (chatops.Result, error) {
	st, ok := o.imp.Staged(a.Token)
	if !ok {
		return chatops.Result{}, chatops.Refuse(gone)
	}
	m, err := o.imp.Commit(ctx, loraimport.Input{Token: a.Token, Name: a.Name, Meta: defaults(st.Meta)})
	var ref *chatops.Refusal
	if errors.As(err, &ref) {
		o.imp.Discard(a.Token)
		return chatops.Result{}, chatops.Refuse("%s\nファイルは破棄しました。値を直す必要があるときは、「モデルの管理」の画面の「LoRA を追加」から追加してください", ref.Message)
	}
	if err != nil {
		return chatops.Result{}, err
	}
	return chatops.Result{Text: "「" + m.Name + "」を LoRA として追加しました。全プロジェクトで、単発生成と各 Project の生成の LoRA として選べます。" +
		"推奨の強さ・ベースモデル・トリガー語・ライセンスは「モデルの管理」の画面で直せます。"}, nil
}

// defaults is what the model manager form would submit without edits: what the file says of itself.
func defaults(m lorafile.Metadata) loraimport.Meta {
	return loraimport.Meta{BaseModels: m.BaseModels, TriggerWords: strings.Join(m.TriggerWords, ", "), LicenseName: m.License}
}

func text(content string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindText, Content: content}
}

// refusal is the reason a file was not taken.
type refusal struct{ file, reason string }

// text is the reason, with the name of the file when the message carries several.
func (r refusal) text(named bool) string {
	if named {
		return r.file + ": " + r.reason
	}
	return r.reason
}

// refusedText is the reason when every file was refused: a line per file for several.
func refusedText(refused []refusal, named bool) string {
	lines := make([]string, len(refused))
	for i, r := range refused {
		lines[i] = r.text(named)
	}
	return strings.Join(lines, "\n")
}
