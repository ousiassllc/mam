package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/stateextractor"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// The promotion of a Project note into the World State (spec/spec.md §9.4): the State Extractor proposes it in the
// background, and the Approval card that carries it is settled here.

// proposeInBackground lets the State Extractor look at the new version of a Project note after the
// request has been answered. Only Project notes are promoted (Series notes copy into a Project).
func (s *NoteService) proposeInBackground(ctx context.Context, n domain.NoteView) {
	if n.ProjectID == "" || s.extractor == nil {
		return
	}
	s.proposals.Go(s.logger, "propose note promotion", func() {
		if err := s.ProposePromotion(context.WithoutCancel(ctx), n.ID, n.Version.Number); err != nil {
			s.logger.Error("propose note promotion failed", "note_id", n.ID, "version", n.Version.Number, "err", err)
		}
	})
}

// ProposePromotion compares the version of a Project note with the World State and, when it says
// something that fits the structured fields, posts a non-blocking Approval card proposing to write it
// there (spec/spec.md §9.4). A version is proposed once: a version that was proposed before, a Series
// note, or a version with nothing structurable posts nothing. A version whose structurable fields are
// all LOCKed still posts one card naming them as left out; approving it writes no World State.
func (s *NoteService) ProposePromotion(ctx context.Context, noteID string, version int) error {
	if s.extractor == nil {
		return nil
	}
	n, err := s.notes.GetVersion(ctx, noteID, version)
	if err != nil {
		return notFound(err)
	}
	if n.ProjectID == "" {
		return nil
	}
	world, err := s.world.World(ctx, n.ProjectID, "")
	if err != nil {
		return notFound(err)
	}
	locks, err := s.world.Locks(ctx, n.ProjectID)
	if err != nil {
		return err
	}
	found, err := s.extractor.Promote(ctx, stateextractor.PromotionInput{Note: n, World: world, Locks: locks})
	if err != nil {
		return err
	}
	if found.Empty() && len(found.Conflicts.Changes) == 0 {
		return nil
	}
	_, card, err := s.notes.CreatePromotion(ctx,
		repository.NewPromotion{NoteVersionID: n.Version.ID, Proposed: found.Proposed, Conflicts: found.Conflicts},
		domain.ChatMessage{Content: promotionText(n.Version.Title, world, found)})
	if errors.Is(err, repository.ErrConflict) {
		return nil
	}
	if err != nil {
		return notFound(err)
	}
	s.publishCreated(n.ProjectID, []domain.ChatMessage{card})
	return nil
}

// promotionText is the content of a promotion card: what would be written, and what is left out
// because it is LOCKed.
func promotionText(title string, world domain.WorldContext, p stateextractor.Promotion) string {
	var b strings.Builder
	fmt.Fprintf(&b, "メモ「%s」から、World State に反映できる項目を見つけました。承認すると World State に新しいバージョンが追加されます。", title)
	for _, c := range p.Proposed.Changes {
		b.WriteString("\n・" + changeText(world, c))
	}
	if len(p.Conflicts.Changes) > 0 {
		b.WriteString("\nLOCK 中のため未反映:")
		for _, c := range p.Conflicts.Changes {
			b.WriteString("\n・" + changeText(world, c))
		}
	}
	return b.String()
}

func changeText(world domain.WorldContext, c domain.StateChange) string {
	return fmt.Sprintf("%s %s: %s → %s", entityName(world, c), c.Field, jsonText(c.From), jsonText(c.To))
}

func entityName(world domain.WorldContext, c domain.StateChange) string {
	switch c.Target {
	case domain.TargetCharacter:
		if ch, ok := world.Character(c.ID); ok {
			return ch.Name
		}
	case domain.TargetProduct:
		if p, ok := world.Product(c.ID); ok {
			return p.Name
		}
	case domain.TargetLocation:
		if l, ok := world.Location(c.ID); ok {
			return l.Name
		}
	}
	return string(c.Target)
}

func jsonText(raw []byte) string {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if text == "" || text == "null" {
		return "（未設定）"
	}
	return text
}

// settlePromotion answers the promotion behind an Approval card (ref_type note_promotion): approving
// writes it into the trunk World State, requesting changes rejects it (F-31). It returns the AI's
// answer for the thread. A promotion that was answered already (the card update failed earlier)
// only gets the answer.
func (s *NoteService) settlePromotion(ctx context.Context, card domain.ChatMessage, decision Decision) (string, error) {
	approve := decision == DecisionApprove
	res, err := s.notes.ResolvePromotion(ctx, card.RefID, approve)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return "", ErrNotFound
	case errors.Is(err, repository.ErrConflict):
		return "この提案はすでに処理されています。", nil
	case err != nil:
		return "", err
	}
	if !approve {
		return "提案を却下しました。メモはそのまま残ります。", nil
	}
	promo, err := s.notes.Promotion(ctx, card.RefID)
	if err != nil {
		return "", err
	}
	world, err := s.world.World(ctx, promo.ProjectID, "")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if len(res.Applied.Changes) == 0 {
		b.WriteString("反映できる項目はありませんでした。")
	} else {
		fmt.Fprintf(&b, "承認されました。World State に反映しました（バージョン %d）。", res.Version)
		for _, c := range res.Applied.Changes {
			b.WriteString("\n・" + changeText(world, c))
		}
	}
	if len(res.Skipped.Changes) > 0 {
		b.WriteString("\nLOCK 中のため未反映:")
		for _, c := range res.Skipped.Changes {
			b.WriteString("\n・" + changeText(world, c))
		}
	}
	return b.String(), nil
}
