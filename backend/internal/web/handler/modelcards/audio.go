package modelcards

import (
	"slices"

	"github.com/a-h/templ"

	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/web/atoms"
	"github.com/ousiassllc/mam/backend/internal/web/molecules/modelcard"
)

// audioRole is the role of the audio input in modelcatalog.Info.RequiresInputs (the service's gentest.RoleAudio; the tests pin that they are the same).
const audioRole = "audio"

// AudioBadge says a speech-to-video model (Wan 2.2 S2V) cannot run without an audio file: the way a card tells the user, before they choose it, to attach one (Issue #1153).
// ok is false for a model that does not need one.
func AudioBadge(m modelcatalog.Info) (b modelcard.Badge, ok bool) {
	if !slices.Contains(m.RequiresInputs, audioRole) {
		return b, false
	}
	return modelcard.Badge{
		Tone: atoms.BadgeInfo, Label: "音声ファイルが必要（S2V）",
		Attrs: templ.Attributes{"data-testid": "gt-model-audio", "title": "音声ファイル（WAV・MP3・FLAC・OGG・M4A）を添えると、その音声と同じ長さの動画を作ります（長さは選べません。上限は models.json の gentest.max_audio_seconds、既定 60 秒）。音声がないと実行できません"},
	}, true
}
