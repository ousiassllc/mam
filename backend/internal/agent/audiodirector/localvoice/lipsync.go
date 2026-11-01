package localvoice

import "errors"

// LipSyncProvider is who redoes the mouths of a run (MAX QUALITY only).
type LipSyncProvider string

// The lip-sync providers. The zero value is Sync Labs: a run that does not choose behaves as it did before the local one existed.
const (
	LipSyncSyncLabs LipSyncProvider = "synclabs"
	LipSyncLocal    LipSyncProvider = "local"
)

// Valid reports whether p is a lip-sync provider of the Audio Director ("" selects Sync Labs).
func (p LipSyncProvider) Valid() bool { return p == "" || p == LipSyncSyncLabs || p == LipSyncLocal }

// InvalidProvider names the option of a run that is not a provider of the Audio Director ("voice_provider" or "lip_sync_provider"), and
// is "" when both are.
func InvalidProvider(voice Provider, lipSync LipSyncProvider) string {
	switch {
	case !voice.Valid():
		return "voice_provider"
	case !lipSync.Valid():
		return "lip_sync_provider"
	}
	return ""
}

// ErrUnknownLipSyncProvider is a run that names a lip-sync provider the Audio Director does not have.
var ErrUnknownLipSyncProvider = errors.New("audiodirector: unknown lip sync provider")

// LipSyncReason is the sentence for the user of the failure of a local lip sync, as it is listed in Skipped: the Detail of the
// error when it is not an *UnavailableError. It is the first version: the final wording belongs to the provider settings (#924);
// the Kind of the error is what a screen branches on.
func LipSyncReason(err error) string {
	var un *UnavailableError
	if !errors.As(err, &un) {
		return err.Error()
	}
	head := map[Kind]string{
		KindNotInstalled:   "ローカルの口パク（MuseTalk 1.5）が導入されていません。モデル管理から導入してください",
		KindWeightsMissing: "ローカルの口パク（MuseTalk 1.5）の重みが取得されていません。モデル管理から「重みを導入」を実行してください",
		KindUnsupported:    "この PC ではローカルの口パク（MuseTalk 1.5）を使えません",
		KindGPUMemory:      "ローカルの口パク（MuseTalk 1.5）に GPU のメモリが足りません",
		KindProcess:        "ローカルの口パク（MuseTalk 1.5）が動きませんでした",
		KindTimeout:        "ローカルの口パク（MuseTalk 1.5）が時間内に終わりませんでした",
		KindNoFace:         "動画から顔を見つけられなかったので、口パクを作れませんでした",
		KindTooLarge:       "動画が大きすぎて、ローカルの口パク（MuseTalk 1.5）で扱えませんでした",
	}[un.Kind]
	if head == "" {
		head = "ローカルの口パク（MuseTalk 1.5）を使えません"
	}
	if un.Detail != "" {
		head += "（" + un.Detail + "）"
	}
	return head
}
