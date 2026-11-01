package settings

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/audiosettings"
)

// LocalProvider is one local provider (GPT-SoVITS for the voice, MuseTalk 1.5 for the lip sync): where it stands and, when it cannot be used,
// the sentence the screen and a failed run say.
type LocalProvider struct {
	Available     bool   `json:"available" jsonschema:"false ならこのアプリにはローカルの実行環境がない。ほかの項目は空"`
	Name          string `json:"name,omitempty" jsonschema:"モデルの名前（GPT-SoVITS, MuseTalk 1.5）"`
	RuntimeID     string `json:"runtime_id,omitempty" jsonschema:"モデルマネージャーの実行環境の id。導入・起動は model-manager のツールで行う"`
	State         string `json:"state,omitempty" jsonschema:"unsupported（この PC では動かせない）, installing, not_installed, weights_missing, ready, starting, running or failed"`
	Usable        bool   `json:"usable" jsonschema:"今すぐ使える（導入済み・重みを確認済み・この PC で動かせる）"`
	Reason        string `json:"reason,omitempty" jsonschema:"usable が false のとき、画面と実行の失敗が言うのと同じ文（直し方とクラウドに切り替える方法つき）"`
	SupportReason string `json:"support_reason,omitempty" jsonschema:"state が unsupported のとき、動かせない理由と代わりに使うクラウドのプロバイダー（macOS はここに理由が出る）"`
	ProcessError  string `json:"process_error,omitempty" jsonschema:"state が failed のとき、サーバーが起動しなかった理由（ログの末尾）"`
	VRAMMinGB     int    `json:"vram_min_gb,omitempty" jsonschema:"必要なビデオメモリ（GB）。vram_estimate が true のときは mam の推定で、実機では未確認"`
	VRAMEstimate  bool   `json:"vram_estimate,omitempty"`
	SourceURL     string `json:"source_url,omitempty" jsonschema:"プログラムの入手元"`
	License       string `json:"license,omitempty" jsonschema:"プログラムのライセンス"`
	LicenseURL    string `json:"license_url,omitempty"`
	Condition     string `json:"commercial_condition,omitempty" jsonschema:"商用利用の条件の注記（あれば）"`
	// UnclearCommercialUse are the weights and dependencies whose commercial use is not clear; the list with sources is in the model manager.
	UnclearCommercialUse []string `json:"unclear_commercial_use,omitempty" jsonschema:"商用利用がはっきりしない重み・依存ファイルの名前（出典とライセンスは list_local_runtimes）"`
	CloudProvider        string   `json:"cloud_provider" jsonschema:"代わりに使うクラウドのプロバイダー（API キーの provider 名）: elevenlabs か synclabs"`
	CloudKeySet          bool     `json:"cloud_key_set" jsonschema:"そのクラウドの API キーが設定済みか（値は返さない）"`
}

// AudioProvidersSection is the "音声・口パク" section.
type AudioProvidersSection struct {
	Available       bool          `json:"available" jsonschema:"false ならこのアプリには音声・口パクの設定がない"`
	Problem         string        `json:"problem,omitempty" jsonschema:"設定ファイル（audio.json）を読めない理由。あるときは既定を示し、実行は止まる"`
	VoiceProvider   string        `json:"voice_provider" jsonschema:"声: elevenlabs（既定）か local（GPT-SoVITS）"`
	LipSyncProvider string        `json:"lip_sync_provider" jsonschema:"口パク: synclabs（既定）か local（MuseTalk 1.5）"`
	FallbackToCloud bool          `json:"fallback_to_cloud" jsonschema:"ローカルが使えないとき、実行の開始前の検査でクラウドに切り替える（既定はオフ）。切り替えるとクラウドの費用が出る"`
	Voice           LocalProvider `json:"voice"`
	LipSync         LocalProvider `json:"lip_sync"`
}

func localProvider(l *audiosettings.Local, role localvoice.Role, keySet bool) LocalProvider {
	out := LocalProvider{CloudProvider: role.CloudKeyID(), CloudKeySet: keySet}
	if l == nil {
		return out
	}
	out.Available, out.Name, out.RuntimeID, out.State, out.Usable, out.Reason = true, l.Name, l.RuntimeID, string(l.State), l.Usable, l.Reason
	out.SupportReason, out.ProcessError, out.VRAMMinGB, out.VRAMEstimate = l.SupportReason, l.ProcessError, l.VRAMMinGB, l.VRAMEstimate
	out.SourceURL, out.License, out.LicenseURL, out.Condition, out.UnclearCommercialUse = l.SourceURL, l.LicenseName, l.LicenseURL, l.Condition, l.Unclear
	return out
}

func (h handlers) audioSection(ctx context.Context) (AudioProvidersSection, error) {
	svc := h.r.Settings.Audio
	if svc == nil {
		return AudioProvidersSection{}, unavailable("音声・口パクの設定")
	}
	v, err := svc.View(ctx)
	if err != nil {
		return AudioProvidersSection{}, err
	}
	return AudioProvidersSection{
		Available: true, Problem: v.Problem, VoiceProvider: string(v.Settings.VoiceProvider), LipSyncProvider: string(v.Settings.LipSyncProvider),
		FallbackToCloud: v.Settings.FallbackToCloud,
		Voice:           localProvider(v.Voice, localvoice.RoleVoice, v.VoiceKeySet),
		LipSync:         localProvider(v.LipSync, localvoice.RoleLipSync, v.LipSyncKeySet),
	}, nil
}

func (h handlers) getAudio(ctx context.Context, _ Input) (AudioProvidersSection, error) {
	return h.audioSection(ctx)
}

// SetAudioInput lists the choices to change. A choice that is not given keeps what is saved.
type SetAudioInput struct {
	VoiceProvider   *string `json:"voice_provider,omitempty" jsonschema:"声のプロバイダー: elevenlabs か local。この PC で動かせないときに local を選ぶと validation（理由つき）"`
	LipSyncProvider *string `json:"lip_sync_provider,omitempty" jsonschema:"口パクのプロバイダー: synclabs か local。この PC で動かせないときに local を選ぶと validation（理由つき）"`
	FallbackToCloud *bool   `json:"fallback_to_cloud,omitempty" jsonschema:"ローカルが使えないとき、実行の開始前の検査でその実行全体をクラウドで作る。オンにするとクラウドの API 費用が出ることがある"`
}

func (h handlers) setAudio(ctx context.Context, in SetAudioInput) (AudioProvidersSection, error) {
	svc := h.r.Settings.Audio
	if svc == nil {
		return AudioProvidersSection{}, unavailable("音声・口パクの設定")
	}
	if in.VoiceProvider == nil && in.LipSyncProvider == nil && in.FallbackToCloud == nil {
		return AudioProvidersSection{}, invalidInput("", "変える項目（voice_provider・lip_sync_provider・fallback_to_cloud）を 1 つ以上指定してください")
	}
	u := audiosettings.Update{FallbackToCloud: in.FallbackToCloud}
	if in.VoiceProvider != nil {
		u.VoiceProvider = new(localvoice.Provider(*in.VoiceProvider))
	}
	if in.LipSyncProvider != nil {
		u.LipSyncProvider = new(localvoice.LipSyncProvider(*in.LipSyncProvider))
	}
	var bad *audiosettings.InvalidError
	switch err := svc.Save(ctx, u); {
	case err == nil:
	case errors.As(err, &bad):
		return AudioProvidersSection{}, invalidInput(bad.Field, bad.Message)
	case errors.Is(err, audiosettings.ErrUnreadable):
		return AudioProvidersSection{}, &service.ConflictError{Message: audiosettings.UnreadableMessage}
	default:
		return AudioProvidersSection{}, err
	}
	return h.audioSection(ctx)
}
