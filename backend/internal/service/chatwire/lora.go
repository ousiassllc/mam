package chatwire

import (
	"context"
	"io"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/lora"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/loraimport"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// The chat operation that takes an attached .safetensors file in as a LoRA (service/chatops/lora, #1140): the same LoRA importer the model manager screen
// (POST /model-manager/loras/inspect and /model-manager/loras) and the MCP upload purpose model_lora_import use. Without the model manager the file is not taken and the
// chat refuses it as an attachment it does not support.
func init() {
	builders = append(builders, func(env Env, reg *chatops.Runtime) {
		if env.ModelManager == nil || env.ModelManager.LoRAs == nil {
			return
		}
		offer := lora.New(loraImporter{env.ModelManager.LoRAs})
		reg.Files(offer)
		reg.Handle(lora.OpAdd, offer)
		reg.Handle(lora.OpDiscard, offer)
	})
}

// loraImporter reports what the importer refuses a user can act on (not a LoRA, too big, no room, too many waiting, a name that is taken) as *chatops.Refusal, with the
// sentence the model manager screen shows.
type loraImporter struct{ *loraimport.Service }

func (i loraImporter) Stage(ctx context.Context, name string, declared int64, r io.Reader) (loraimport.Staged, error) {
	st, err := i.Service.Stage(ctx, name, declared, r)
	return st, loraRefusal(err)
}

func (i loraImporter) Commit(ctx context.Context, in loraimport.Input) (loraimport.Model, error) {
	m, err := i.Service.Commit(ctx, in)
	return m, loraRefusal(err)
}

func loraRefusal(err error) error {
	if kind, msg := mmview.Refuse(err); kind != mmview.RefusalNone {
		return &chatops.Refusal{Message: msg}
	}
	return err
}
