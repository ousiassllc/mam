package shotaudio

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// Shot is a Shot of the Project as the operation needs it: its number as the thread says it, and its audio.
type Shot struct {
	Scene, Number int // 1-based
	// HasAudio says the Shot is tied to an audio asset; Seconds is its length (0: unknown).
	HasAudio bool
	Seconds  float64
	// Problem is why the tied audio cannot be used now ("": it can).
	Problem string
}

// Deps is what the operation needs of the service layer: shotaudiosvc.Service, the function the Shot screen and the MCP tools call. The functions report what the screen reports (a
// LOCKed Shot, a Shot that is being generated, an asset that is no audio) as *chatops.Refusal.
type Deps struct {
	// Shot is the Shot of the Project, as it is now.
	Shot func(ctx context.Context, projectID, shotID string) (Shot, error)
	// Set ties the Shot to the audio asset of the Project ("": takes the audio off) and returns the Shot as it is then.
	Set func(ctx context.Context, projectID, shotID, assetID string) (Shot, error)
}

// Handler is the Handler of OpSetShotAudio.
type Handler struct{ deps Deps }

// New returns the handler of OpSetShotAudio.
func New(d Deps) *Handler { return &Handler{deps: d} }

const (
	needAudio = "音声ファイルが添付されていません。WAV・MP3・FLAC・OGG・M4A の音声ファイルを 1 つ添付して、同じメッセージで「この音声を Shot 2 に使って」のように送ってください（アップロード済みの音声は、Shot の画面の「音声」欄から選べます）。"
	needShot  = "どの Shot の音声か分かりません。Take カードに返信するか、「Scene 1 の Shot 2」のように指定してください。"
)

// Question is never asked: the operation has no card (Spec.Immediate).
func (h *Handler) Question(context.Context, domain.Project, commandinterpreter.Operation) (chatops.Card, error) {
	return chatops.Card{}, fmt.Errorf("the operation %q has no card", OpSetShotAudio)
}

// Apply sets or clears the audio of the Shot the direction is about (chatops.ShotID). "attach" takes the one audio file the message carries (a message with none, or with several,
// is refused: the Shot must be made from the file the director meant); the file must already be an asset of the Project (the chat stores an attachment as one).
func (h *Handler) Apply(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a Args
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	shotID := chatops.ShotID(ctx)
	if shotID == "" {
		return chatops.Result{}, chatops.Refuse("%s", needShot)
	}
	before, err := h.deps.Shot(ctx, project.ID, shotID)
	if err != nil {
		return chatops.Result{}, err
	}
	if a.Action == ActionClear {
		if !before.HasAudio {
			return chatops.Result{Text: where(before) + " には音声が付いていません。変更しませんでした。"}, nil
		}
		if _, err := h.deps.Set(ctx, project.ID, shotID, ""); err != nil {
			return chatops.Result{}, err
		}
		return chatops.Result{Text: where(before) + " の音声を外しました。次の生成から、音声を使わない通常の動画モデルで作ります。できあがった Take は変わりません。"}, nil
	}
	asset, err := attachedAudio(ctx)
	if err != nil {
		return chatops.Result{}, err
	}
	after, err := h.deps.Set(ctx, project.ID, shotID, asset.ID)
	if err != nil {
		return chatops.Result{}, err
	}
	text := where(after) + " の音声を設定しました"
	if after.Seconds > 0 {
		text += fmt.Sprintf("（%.1f 秒）", after.Seconds)
	}
	text += "。次の生成から、音声を受け取る動画モデル（Wan 2.2 S2V 14B など）が、参照画像（Keyframe）とこの音声から動画を作ります。動画の長さは音声の長さになります。できあがった Take は変わりません。"
	if after.Problem != "" {
		text += "\n注意: " + after.Problem
	}
	return chatops.Result{Text: text}, nil
}

// where is "Scene 1 / Shot 2".
func where(s Shot) string { return fmt.Sprintf("Scene %d / Shot %d", s.Scene, s.Number) }

// attachedAudio is the one audio file of this message; a message without one, or with several, is refused.
func attachedAudio(ctx context.Context) (domain.Asset, error) {
	var found []domain.Asset
	for _, a := range chatops.Attachments(ctx) {
		if a.Type == domain.AssetAudio {
			found = append(found, a)
		}
	}
	switch len(found) {
	case 0:
		return domain.Asset{}, chatops.Refuse("%s", needAudio)
	case 1:
		return found[0], nil
	}
	return domain.Asset{}, chatops.Refuse("音声ファイルが %d 個添付されています。Shot に付ける音声を 1 つだけ添付して送ってください", len(found))
}
