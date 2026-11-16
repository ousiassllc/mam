package workflows

import (
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/audioin"
)

// audioOf is the sound of a run (Issue #1153), checked against what the workflow takes: a workflow with an audio assignment cannot run without a sound, and one
// without cannot be given one (it is refused, not dropped). The kind is read from the bytes, as for a single generation; the length of the sound is read, and refused when it is
// too long or unreadable, where the clip is as long as the sound (audiolength.Apply in prepare).
func audioOf(saved localcfg.SavedWorkflow, data []byte) (*modeladapter.Input, error) {
	takes := saved.Model.Workflow != nil && saved.Model.Media == localcfg.MediaVideo && saved.Model.Workflow.Has(localcfg.ParamAudio)
	switch {
	case len(data) == 0 && takes:
		return nil, invalid("このワークフローは音声ファイルが要ります。音声ファイルを渡してください（%s）", audioin.KindsText)
	case len(data) == 0:
		return nil, nil
	case !takes:
		return nil, invalid("このワークフローは音声ファイルを使いません")
	case len(data) > audiofile.MaxBytes:
		return nil, invalid("音声ファイルは 1 バイト以上 %d MiB 以下にしてください", audiofile.MaxBytes>>20)
	}
	mime, ok := audiofile.Sniff(data)
	if !ok {
		return nil, invalid("%s", audioin.NotAudioMessage)
	}
	return &modeladapter.Input{Data: data, MimeType: mime}, nil
}
