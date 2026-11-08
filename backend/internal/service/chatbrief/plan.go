// Package chatbrief decides what a chat message means for the Creative Brief of a Project (spec/spec.md §4.2): an answer to a
// question, the confirmation of the batch summary, or what the words say about the Brief.
package chatbrief

import (
	"context"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Planner reads and writes nothing itself: it plans the Brief part of a turn.
type Planner struct {
	Projects  *repository.ProjectRepository
	Chat      *repository.ChatRepository
	Extractor briefing.Extractor
}

// Plan adds the Brief and the assistant's follow-up to the turn: the answer to a question, the confirmation of the batch summary,
// or what the words say about the Brief (briefing.Absorb). It returns the items the message decided, whose question cards are now
// closed. A Project that does not exist is repository.ErrNotFound.
func (s Planner) Plan(ctx context.Context, project domain.Project, turn *repository.Turn, target *domain.ChatMessage, replied map[string]bool, text string, attached []domain.Asset) ([]domain.BriefField, error) {
	brief, err := s.Projects.GetBrief(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	before := briefing.Clone(brief)
	brief.AttachAssets(attached)
	attachedOnly := briefing.Clone(brief) // a file alone is no reason for the AI to speak again

	drafting := project.State == domain.StateDraft
	var refused *domain.ChatMessage
	if drafting || (project.State == domain.StatePlanning && len(brief.Missing()) > 0) { // PLANNING too: a script held back for the Subject (#1108)
		if refused, err = s.answerBrief(ctx, project, &brief, target, replied, text); err != nil {
			return nil, err
		}
	}
	if refused != nil {
		turn.Messages = append(turn.Messages, *refused)
	}
	// A reply to the summary that did not confirm it closes that card (it has a reply now): the summary is shown again, with what the
	// reply changed, so that one is always open while the Brief waits for the confirmation.
	reasked := drafting && target != nil && target.RefType == domain.RefBriefConfirm && !replied[target.ID] && briefing.NeedsConfirmation(brief)
	if !briefing.Changed(before, brief) && !reasked {
		return nil, nil
	}
	if briefing.Changed(before, brief) {
		turn.Brief = &brief
	}
	// The AI speaks again only when the message moved the Brief forward (a refused answer asks again).
	beforeMissing, nowMissing := before.Missing(), brief.Missing()
	if refused == nil && (reasked || (drafting && briefing.Changed(attachedOnly, brief)) || (!drafting && !slices.Equal(beforeMissing, nowMissing))) {
		turn.Messages = append(turn.Messages, briefing.FollowUp(brief, project.ConfirmationLevel, drafting))
	}
	decided := slices.DeleteFunc(slices.Clone(beforeMissing), func(f domain.BriefField) bool { return slices.Contains(nowMissing, f) })
	if briefing.NeedsConfirmation(before) && !briefing.NeedsConfirmation(brief) {
		decided = append(decided, domain.BriefField(domain.RefBriefConfirm)) // the summary was answered in words
	}
	if before.ProposedSubject != "" && brief.ProposedSubject == "" {
		decided = append(decided, domain.BriefSubject) // the contradiction was settled
	}
	return decided, nil
}

// answerBrief applies a message received while the Brief is drafted (or the Subject is missing); it returns the question again for an answer the Brief cannot hold.
func (s Planner) answerBrief(ctx context.Context, project domain.Project, brief *domain.Brief, target *domain.ChatMessage, replied map[string]bool, text string) (*domain.ChatMessage, error) {
	if target != nil && target.Kind == domain.KindQuestion && target.RefType == domain.RefBriefField && !replied[target.ID] {
		return briefing.ApplyAnswer(brief, domain.BriefField(target.RefID), text), nil
	}
	if project.State == domain.StateDraft && briefing.NeedsConfirmation(*brief) && briefing.IsConfirmation(text) {
		briefing.Confirm(brief) // "この内容でよい": the summary is the Brief as it stands
		return nil, nil
	}
	// Free input: take what the text says about the Brief (never replacing what the user decided unless the text says it changes it),
	// then treat the rest as the answer to the newest open question.
	beforeAbsorb := briefing.Clone(*brief)
	briefing.Absorb(brief, s.Extractor.Extract(text), text)
	said := briefing.Changed(beforeAbsorb, *brief) // the text stated something: it is not also the answer to the open question
	briefing.Recommend(brief)
	if said {
		return nil, nil
	}
	open, err := s.Chat.LatestOpenQuestion(ctx, project.ID)
	if err != nil || open == nil {
		return nil, err
	}
	f := domain.BriefField(open.RefID)
	if f == domain.BriefSubject && brief.ProposedSubject != "" {
		// A contradiction is settled by naming one of the two values, not by any remark.
		if t := strings.TrimSpace(text); t != brief.Subject && t != brief.ProposedSubject {
			return nil, nil
		}
	} else if !slices.Contains(brief.Missing(), f) {
		return nil, nil
	}
	return briefing.ApplyAnswer(brief, f, text), nil
}

// Open reports whether the Brief question card m can still be answered: nobody replied to it and what it asks is still open — the item
// is missing, the Subject is contradicted, or (the summary card) the Project still drafts a Brief that waits for the confirmation.
func Open(m domain.ChatMessage, replied bool, b domain.Brief, state domain.ProjectState) bool {
	if replied {
		return false
	}
	if m.RefType == domain.RefBriefConfirm {
		return state == domain.StateDraft && briefing.NeedsConfirmation(b)
	}
	f := domain.BriefField(m.RefID)
	return slices.Contains(b.Missing(), f) || (f == domain.BriefSubject && b.ProposedSubject != "")
}
