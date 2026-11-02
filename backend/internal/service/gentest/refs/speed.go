package refs

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// WarnSpeedUnsupported is the code of the warning that a video model has no such speed mode: it runs its default.
const WarnSpeedUnsupported = "speed_mode_unsupported"

// CheckSpeedMode checks the speed mode of a request ("" is the model's default) and returns it trimmed. A speed mode is for video
// models only and must look like an ID (localcfg.ValidSpeedModeID); which model has it is a warning of the estimate, not a refusal.
func CheckSpeedMode(media, id string) (string, error) {
	id = strings.TrimSpace(id)
	switch {
	case id == "":
		return "", nil
	case media != gentestrepo.MediaVideo:
		return "", gterr.Invalid("速さのモード（speed_mode）は動画だけで使えます")
	case !localcfg.ValidSpeedModeID(id):
		return "", gterr.Invalid("速さのモードは fast や standard のような名前で指定してください")
	}
	return id, nil
}
