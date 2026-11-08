package generation

import (
	"context"
	"fmt"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// kindLabel names a reference kind in the cards of the chat.
var kindLabel = map[domain.ReferenceKind]string{
	domain.RefCharacter: "Character Reference",
	domain.RefProduct:   "Product Reference",
	domain.RefLocation:  "Location Reference",
	domain.RefStyle:     "Style Reference",
	domain.RefLighting:  "Lighting Reference",
	domain.RefKeyframe:  "Keyframe",
}

// announceKeyframes posts a card to the chat for each image this run generated.
func (o *Orchestrator) announceKeyframes(ctx context.Context, b *packBuild) {
	for _, r := range b.results {
		if r.reused {
			continue
		}
		label := kindLabel[r.plan.Kind]
		if r.plan.Name != "" {
			label += "（" + r.plan.Name + "）"
		}
		card := domain.ChatMessage{
			Role: domain.RoleAssistant, Kind: domain.KindKeyframe, RefID: r.asset.ID,
			Content: fmt.Sprintf("Shot %d: %s（%s・%s）", b.ref.Shot.OrderIndex+1, label, r.output.Model, r.use.Label()),
		}
		if err := o.chat.Announce(context.WithoutCancel(ctx), b.ref.ProjectID, card); err != nil {
			o.logger.Error("post keyframe card failed", "asset_id", r.asset.ID, "err", err)
		}
	}
}
