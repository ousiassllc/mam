package install

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// consentLabel is the button of a card that asks the user to accept a license: it is the acceptance, and the card shows the license it is for.
const consentLabel = "同意して導入する"

// globalNote ends every card: an install is a setting of the whole app.
const globalNote = "・導入はアプリ全体の設定です。全プロジェクトで使えるようになり、ディスクの空きとネットワークを使います。"

// bytesText writes a size the way the model manager shows it: 1.2 GB, 340 MB.
func bytesText(n int64) string {
	const mb, gb = 1 << 20, 1 << 30
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	case n >= mb:
		return fmt.Sprintf("%.0f MB", float64(n)/mb)
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}

// signed is a free space as the int64 the sizes are written in (a disk is never larger than that).
func signed(free uint64) int64 { return int64(min(free, math.MaxInt64)) } //nolint:gosec // clamped just above

// commercialText is the license.commercial of the catalog in words, with the condition of a conditional model.
func commercialText(l License) string {
	label := map[string]string{"yes": "商用OK", "conditional": "条件つき", "no": "商用NG"}[l.Commercial]
	if label == "" {
		label = "不明"
	}
	if l.Condition != "" && l.Commercial != "yes" {
		return label + "（" + l.Condition + "）"
	}
	return label
}

// licenseLines are the lines of a card that say what the license is and whether the model may be used commercially.
func licenseLines(l License) string {
	name := l.Name
	if name == "" {
		name = "ライセンスの記載なし"
	}
	out := "・ライセンス: " + name
	if l.ID != "" && l.Version != "" {
		out += "（" + l.ID + " " + l.Version + "）"
	}
	out += "\n・商用利用: " + commercialText(l) + "\n"
	if l.Notice != "" {
		out += "・注意: " + l.Notice + "\n"
	}
	if len(l.ExcludedRegions) > 0 {
		out += "・次の地域ではこのライセンスは使えません: " + strings.Join(l.ExcludedRegions, "、") + "\n"
	}
	return out
}

// sizeLine says what is fetched and, when the room is known, whether it fits.
func sizeLine(total, need int64, free uint64) string {
	out := "・ダウンロード: " + bytesText(total)
	if need != total {
		out += "（これから取得する分 " + bytesText(need) + "）"
	}
	if free > 0 {
		out += "。導入先の空き容量: " + bytesText(signed(free))
	}
	return out + "\n"
}

// room refuses an install that needs more than the free space (the service refuses it too, and says the same; the card must not be shown for it). free 0 is "not known".
func room(need int64, free uint64) error {
	if free == 0 || need <= signed(free) {
		return nil
	}
	return chatops.Refuse("空き容量が足りません（必要 %s、空き %s）。空けてからやり直してください", bytesText(need), bytesText(signed(free)))
}

// environment is why an install that needs the bundled engine cannot be done here, as the MCP tools tell it; nil when it can.
func environment(e Environment) error {
	switch {
	case !e.Supported:
		return refusal(modelmanager.ErrUnsupported)
	case e.Pinned:
		return refusal(modelmanager.ErrPinned)
	case !e.Writable:
		return refusal(store.ErrNotWritable)
	case !e.EngineInstalled:
		return refusal(modelmanager.ErrEngineNotInstalled)
	}
	return nil
}

// refusal tells an error of the service with the sentence the MCP tools give for it. An error that is not a refusal is returned as it is.
func refusal(err error) error {
	if _, msg := mmview.Refuse(err); msg != "" {
		return chatops.Refuse("%s", msg)
	}
	return err
}

// asRefusal is err as *chatops.Refusal when it already is one.
func asRefusal(err error) (*chatops.Refusal, bool) {
	var r *chatops.Refusal
	return r, errors.As(err, &r)
}

// Installing is what the chat says when the install asked for is already running.
func installing(name string) error {
	return chatops.Refuse("「%s」はすでに導入中です。終わるまでお待ちください（進み具合は、導入を始めたときのカードに出ています）", name)
}

// progressMessage is the card that follows the sentence of an install that started: the chat shows how far the job is (Progress) and re-reads it while it runs.
func progressMessage(job Job, name string) domain.ChatMessage {
	return domain.ChatMessage{Role: domain.RoleAssistant, Kind: domain.KindProgress, RefType: domain.RefJob, RefID: job.ID, Content: "「" + name + "」を導入しています"}
}

// started is the answer of an install that was queued: the sentence and the progress card.
func started(job Job, name string) chatops.Result {
	text := "「" + name + "」の導入を始めました。進み具合は下のカードに出ます。導入は全プロジェクトで使えます。"
	if job.Existing {
		text = "「" + name + "」はすでに導入中でした。そのジョブの進み具合を出します。"
	}
	return chatops.Result{Text: text, Messages: []domain.ChatMessage{progressMessage(job, name)}}
}

// choiceCard offers the candidates as one button each; picking one asks the confirmation of that one (the operation of the button carries Pick).
func choiceCard(what string, names []string, ops []commandinterpreter.Operation) chatops.Card {
	card := chatops.Card{Text: "どの" + what + "を導入しますか？ 選ぶと、大きさとライセンスを確かめるカードを出します（まだ何も始めていません）。"}
	for i, n := range names {
		card.Choices = append(card.Choices, chatops.Choice{Label: n, Op: ops[i]})
	}
	return card
}

// tooMany is the refusal for a name that matches more than the buttons of a card can hold.
func tooMany(what string, names []string) error {
	return chatops.Refuse("「%s」に当てはまるものが %d 件あります（%s ほか）。もう少し具体的な名前で言ってください", what, len(names), strings.Join(names[:chatops.MaxChoices], "、"))
}
