package shotgen

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/shotview"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// ProposeOutput is the Continue card posted to the thread.
type ProposeOutput struct {
	ProjectID string   `json:"project_id"`
	MessageID string   `json:"message_id" jsonschema:"チャットに載った質問カードの ID"`
	Text      string   `json:"text" jsonschema:"最終フレーム時点の状態と、選択肢の案内"`
	Options   []string `json:"options" jsonschema:"次の展開の候補。confirm_continue の instruction に渡せる（先頭の「A. 」は付けても外しても良い）"`
}

func propose(svc *service.ContinueService) mcpkit.Handler[ShotInput, ProposeOutput] {
	return func(ctx context.Context, in ShotInput) (ProposeOutput, error) {
		project, card, err := svc.Propose(ctx, in.ShotID)
		if err != nil {
			return ProposeOutput{}, err
		}
		body := domain.DecodeCardBody(card.Content)
		options := body.Options
		if options == nil {
			options = []string{}
		}
		return ProposeOutput{ProjectID: project.ID, MessageID: card.ID, Text: body.Text, Options: options}, nil
	}
}

// ConfirmInput is the choice of a Continue card.
type ConfirmInput struct {
	ShotID      string `json:"shot_id" jsonschema:"続きの元になる、完了した Shot"`
	Instruction string `json:"instruction,omitempty" jsonschema:"選んだ候補、または自分の指示。delegate が true なら無視される"`
	Delegate    bool   `json:"delegate,omitempty" jsonschema:"true で AI に任せる"`
}

// ConfirmOutput is the new Shot and the job that generates it.
type ConfirmOutput struct {
	mcpkit.JobRef
	Shot shotview.Card `json:"shot" jsonschema:"追加された Shot（新しい Scene に入ることもある）"`
}

func confirm(r *mcpkit.Registrar, svc *service.ContinueService) mcpkit.Handler[ConfirmInput, ConfirmOutput] {
	return func(ctx context.Context, in ConfirmInput) (ConfirmOutput, error) {
		var frag service.ShotFragment
		ref, err := r.Jobs.RunResolved(ctx, jobs.KindGenerateShot, func(ctx context.Context) (string, string, error) {
			var err error
			frag, err = svc.Confirm(ctx, in.ShotID, service.ConfirmInput{Instruction: in.Instruction, Delegate: in.Delegate})
			return frag.ID, "", err
		})
		if err != nil {
			return ConfirmOutput{}, err
		}
		return ConfirmOutput{JobRef: ref, Shot: shotview.FromFragment(frag)}, nil
	}
}
