package refs

import (
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// WarnSpeedUnsupported is the code of the warning that a model has no such speed mode: it runs its default.
const WarnSpeedUnsupported = "speed_mode_unsupported"

// CheckSpeedMode checks the speed mode of a request ("" is the model's default) and returns it trimmed. It must look like an ID
// (localcfg.ValidSpeedModeID); which model has it is a warning of the estimate, not a refusal.
func CheckSpeedMode(id string) (string, error) {
	id = strings.TrimSpace(id)
	switch {
	case id == "":
		return "", nil
	case !localcfg.ValidSpeedModeID(id):
		return "", gterr.Invalid("速さのモードは fast や standard のような名前で指定してください")
	}
	return id, nil
}
