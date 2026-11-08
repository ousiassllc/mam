package stages

import (
	"context"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/briefing"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// startPlanning moves a DRAFT Project whose Brief is ready to PLANNING (briefing.ReadyToPlan: the Subject is settled, and at the levels
// that confirm, the Brief is confirmed). At FULL AUTO the items nobody decided are left to the AI here and the recommended values are
// adopted as they are, with no confirmation (#1114).
func (s *Pipeline) startPlanning(ctx context.Context, p domain.Project) (bool, error) {
	brief, err := s.projects.GetBrief(ctx, p.ID)
	if err != nil {
		return false, err
	}
	if !briefing.ReadyToPlan(brief, p.ConfirmationLevel) {
		return false, nil
	}
	if len(brief.Missing()) > 0 {
		briefing.LeaveOpenItemsToAI(&brief)
		if err := s.projects.Apply(ctx, repository.Turn{ProjectID: p.ID, Brief: &brief}); err != nil {
			return false, err
		}
	}
	return true, s.projects.Transition(ctx, p.ID, domain.StatePlanning)
}

// subjectNotice is the question that holds the script back until the Brief says what the video is about.
const subjectNotice = "Brief の Subject（題材）がまだ決まっていません。決まるまで脚本は作りません。何（商品・題材）を主役にするか、返信か Brief の Subject で教えてください。"

// subjectKnown reports whether the Brief names what the video is about (domain.Brief.Missing). When it does not, it asks with a
// Subject question card (once: while that card is open nothing is posted again) and the caller stops: a script written without a
// topic is about something the user never asked for (#1108).
func (s *Pipeline) subjectKnown(ctx context.Context, projectID string) (bool, error) {
	brief, err := s.projects.GetBrief(ctx, projectID)
	if err != nil || !slices.Contains(brief.Missing(), domain.BriefSubject) {
		return err == nil, err
	}
	open, err := s.chatRepo.LatestOpenQuestion(ctx, projectID)
	if err != nil || (open != nil && open.RefID == string(domain.BriefSubject)) {
		return false, err
	}
	return false, s.chat.Announce(ctx, projectID, briefing.Question(domain.BriefSubject, subjectNotice+"\n"+domain.BriefSubject.Question()))
}
