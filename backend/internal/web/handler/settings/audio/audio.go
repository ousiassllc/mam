// Package audio is the handlers of the "音声・口パク" category of the settings screen (spec/components/settings-screen-categories.md §9): the
// page body and the save of the choice of providers (PUT /settings/audio).
package audio

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/handler/settings/settingshttp"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/settings/audio"
)

const (
	writeFailed = "設定を保存できませんでした。データフォルダの権限と空き容量を確認してください"
	savedNotice = "保存しました。次の音声作成から使われます"
)

// SectionData is the saved choices and the state of the local providers as the category shows them. A settings file that cannot be read
// is the Problem of the category, so that the rest of the screen still works.
func SectionData(ctx context.Context, svc *audiosettings.Service) view.Data {
	v, err := svc.View(ctx)
	if err != nil {
		return view.Data{Problem: "音声・口パクの状態を読めませんでした。もう一度開いてください"}
	}
	return Data(v)
}

// Data is the category of a View.
func Data(v audiosettings.View) view.Data {
	d := view.Data{Problem: v.Problem, Fallback: v.Settings.FallbackToCloud}
	d.Voice = view.Provider{
		ID: "audio-voice", Title: "声（セリフ・ナレーション）", Field: view.FieldVoice,
		Description: "ドラマ・CM のセリフとナレーションを話す声です。",
		Selected:    string(v.Settings.VoiceProvider),
		CloudValue:  audiosettings.CloudVoice,
		CloudLabel:  "ElevenLabs（クラウド）",
		LocalLabel:  "GPT-SoVITS（この PC）",
		Local:       localOf(v.Voice),
		CloudKey:    audiosettings.VoiceKeyID,
		CloudKeySet: v.VoiceKeySet,
		Note:        "ローカルの声は、Character に設定した参照音声で話します。プロジェクトの「声のサンプルから ElevenLabs でクローン」は ElevenLabs の声で、ローカルの声には使われません。",
	}
	d.LipSync = view.Provider{
		ID: "audio-lip-sync", Title: "口パク", Field: view.FieldLipSync,
		Description: "しゃべるクリップの口元を、音声に合わせて作り直します（MAX QUALITY のときだけ作られます）。",
		Selected:    string(v.Settings.LipSyncProvider),
		CloudValue:  audiosettings.CloudLipSync,
		CloudLabel:  "Sync Labs（クラウド）",
		LocalLabel:  "MuseTalk 1.5（この PC）",
		Local:       localOf(v.LipSync),
		CloudKey:    audiosettings.LipSyncKeyID,
		CloudKeySet: v.LipSyncKeySet,
	}
	return d
}

// localOf is the local provider of a card (nil for an app without the local runtimes).
func localOf(l *audiosettings.Local) *view.Local {
	if l == nil {
		return nil
	}
	out := &view.Local{
		Name: l.Name, Supported: l.Supported, SupportReason: l.SupportReason, Reason: l.Reason, ProcessError: l.ProcessError,
		SourceURL: l.SourceURL, License: l.LicenseName, LicenseURL: l.LicenseURL, Condition: l.Condition, Unclear: l.Unclear,
	}
	out.State, out.Tone = stateText(l.State)
	switch {
	case l.VRAMMinGB == 0:
	case l.VRAMEstimate:
		out.VRAM = fmt.Sprintf("必要なビデオメモリ: %d GB 以上（mam の推定です。実機では確かめていません）", l.VRAMMinGB)
	default:
		out.VRAM = fmt.Sprintf("必要なビデオメモリ: %d GB 以上", l.VRAMMinGB)
	}
	return out
}

// stateText is the badge of a local provider: where it stands.
func stateText(s audiosettings.State) (string, atoms.BadgeTone) {
	switch s {
	case audiosettings.StateUnsupported:
		return "この PC では使えません", atoms.BadgeWarn
	case audiosettings.StateInstalling:
		return "導入中", atoms.BadgeInfo
	case audiosettings.StateNotInstalled:
		return "未導入", atoms.BadgeNeutral
	case audiosettings.StateWeightsMissing:
		return "重みが未取得", atoms.BadgeWarn
	case audiosettings.StateStarting:
		return "起動しています", atoms.BadgeInfo
	case audiosettings.StateRunning:
		return "起動中", atoms.BadgeOK
	case audiosettings.StateFailed:
		return "起動に失敗", atoms.BadgeError
	}
	return "導入済み", atoms.BadgeOK
}

// Save stores the choices of the form (PUT /settings/audio, fields voice_provider, lip_sync_provider and fallback_to_cloud) and answers
// with the category: 200 with the notice, or 422 with the reason when a choice cannot be saved (nothing is then written; the form shows
// what is saved). A provider that is not in the form (a radio that is disabled, because this PC cannot run it) keeps what is saved; the
// checkbox is the whole form's: absent is off.
func Save(svc *audiosettings.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := audiosettings.Update{FallbackToCloud: new(c.PostForm(view.FieldFallback) == "1")}
		if v, ok := c.GetPostForm(view.FieldVoice); ok {
			u.VoiceProvider = audiosettings.VoiceChoice(v)
		}
		if v, ok := c.GetPostForm(view.FieldLipSync); ok {
			u.LipSyncProvider = audiosettings.LipSyncChoice(v)
		}
		err := svc.Save(c.Request.Context(), u)
		d := SectionData(c.Request.Context(), svc)
		status := http.StatusOK
		var bad *audiosettings.InvalidError
		switch {
		case err == nil:
			d.Notice = savedNotice
		case errors.As(err, &bad):
			status, d.Error = http.StatusUnprocessableEntity, bad.Message
		case errors.Is(err, audiosettings.ErrUnreadable):
			status, d.Error = http.StatusConflict, audiosettings.UnreadableMessage
		default:
			_ = c.Error(err)
			status, d.Error = http.StatusInternalServerError, writeFailed
		}
		settingshttp.Render(c, status, view.Section(d))
	}
}
