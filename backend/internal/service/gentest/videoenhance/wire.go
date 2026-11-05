package videoenhance

import (
	"github.com/ousiassllc/mam/backend/internal/agent/cloudenhance"
	"github.com/ousiassllc/mam/backend/internal/agent/topaz"
	"github.com/ousiassllc/mam/backend/internal/credential"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/venhance"
)

// Wiring is what the application hands gentest.Deps for "make a video larger / smoother".
type Wiring struct {
	Cloud cloudenhance.Cloud
	Probe venhance.Prober
}

// Wire builds the paid cloud (Topaz Video API; the key is read from keys at every call) and the ffprobe of the source videos. ffmpeg is nil when ffmpeg /
// ffprobe were not found: then Probe is nil too (not a non-nil interface over a nil pointer), and an enhancement is refused with how to set ffmpeg up.
func Wire(keys credential.Store, ffmpeg *media.Tools) Wiring {
	w := Wiring{Cloud: topaz.New(topaz.DefaultConfig(), keys, nil)}
	if ffmpeg != nil {
		w.Probe = ffmpeg
	}
	return w
}
