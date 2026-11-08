package chatbrief_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// A request that states a theme and constraints but no Subject: the Additional Instruction holds only the extractor's lines.
const noSubjectRequest = "成人で架空の設定。テーマは朝の散歩"

// The theme and constraint lines the extractor wrote are no topic: saving the Brief form or confirming through update_brief makes them the
// user's, yet the Subject is still missing and planning cannot start (#1114).
func TestUpdateBrief_AnInferredInstructionNeverSubstitutesForTheSubject(t *testing.T) {
	for name, form := range map[string]func(current domain.Brief) service.BriefForm{
		"form save": func(b domain.Brief) service.BriefForm {
			return service.BriefForm{Values: map[domain.BriefField]string{}, AdditionalInstruction: b.AdditionalInstruction}
		},
		"update_brief confirm": func(b domain.Brief) service.BriefForm {
			return service.BriefForm{Values: map[domain.BriefField]string{}, AdditionalInstruction: b.AdditionalInstruction, Given: []domain.BriefField{domain.BriefAdditionalInstruction}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: noSubjectRequest})
			require.NoError(t, err)
			before := f.brief(t, p.ID)
			require.Empty(t, before.Subject)
			require.Equal(t, "テーマ: 朝の散歩\n制約: 成人・架空", before.AdditionalInstruction)

			view, err := f.svc.Projects.UpdateBrief(context.Background(), p.ID, form(before))

			require.NoError(t, err)
			assert.Equal(t, domain.BriefSourceUser, view.Brief.SourceOf(domain.BriefAdditionalInstruction), "saved: the user's now")
			assert.Contains(t, view.Brief.Missing(), domain.BriefSubject, "yet no topic")
			assert.Equal(t, domain.StateDraft, view.Project.State, "planning does not start")
		})
	}
}

// Only the newest summary card of the Brief can be answered: an older one shows a Brief that has changed since.
func TestSummaryCards_OnlyTheNewestIsOpen(t *testing.T) {
	f := newFixture(t)
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい。上品な雰囲気で"})
	require.NoError(t, err)

	ws := f.post(t, p.ID, "やっぱりミニマルな雰囲気にして", "") // changes the Brief: a new summary
	require.Equal(t, domain.RefBriefConfirm, ws.Messages[len(ws.Messages)-1].RefType)

	thread, err := f.svc.Chat.Workspace(context.Background(), p.ID)
	require.NoError(t, err)
	var summaries []domain.ThreadMessage
	for _, m := range thread.Messages {
		if m.RefType == domain.RefBriefConfirm {
			summaries = append(summaries, m)
		}
	}
	require.Len(t, summaries, 2)
	assert.False(t, summaries[0].Open, "the older summary is closed")
	assert.True(t, summaries[1].Open)
	updated := false
	for _, m := range ws.Updated {
		updated = updated || (m.ID == summaries[0].ID && !m.Open)
	}
	assert.True(t, updated, "the older summary is returned closed so that the screen redraws it")
}

// update_brief makes the fields it is given the user's, even when the value equals the inferred one (#1114).
func TestUpdateBrief_GivenFieldsBecomeTheUsersEvenWhenEqualToTheInferredOne(t *testing.T) {
	f := newFixture(t)
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液の縦型15秒CMを作りたい"})
	require.NoError(t, err)
	b := f.brief(t, p.ID)
	require.Equal(t, domain.BriefSourceInferred, b.SourceOf(domain.BriefDuration))

	view, err := f.svc.Projects.UpdateBrief(context.Background(), p.ID, service.BriefForm{
		Values:                map[domain.BriefField]string{domain.BriefDuration: b.Duration, domain.BriefSubject: b.Subject, domain.BriefMood: b.Mood},
		AdditionalInstruction: b.AdditionalInstruction, KeepProposals: true,
		Given: []domain.BriefField{domain.BriefDuration, domain.BriefMood},
	})

	require.NoError(t, err)
	assert.Equal(t, domain.BriefSourceUser, view.Brief.SourceOf(domain.BriefDuration), "given, though equal to the inferred value")
	assert.Equal(t, domain.BriefSourceInferred, view.Brief.SourceOf(domain.BriefSubject), "not given: still inferred")
	assert.Empty(t, view.Brief.Mood, "an empty value given is not decided")
}

// A reply closes the card it targets, and a confirmation in words closes the summary although nothing was replied to.
func TestPostMessage_ReturnsTheQuestionCardsItClosed(t *testing.T) {
	f := newFixture(t)
	p, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "動画を作りたい"}) // asks the Subject
	require.NoError(t, err)
	q := f.lastMessage(t, p.ID)

	ws := f.post(t, p.ID, "美容液", q.ID)
	require.Len(t, ws.Updated, 1)
	assert.Equal(t, q.ID, ws.Updated[0].ID)
	assert.False(t, ws.Updated[0].Open)

	open := ws.Messages[len(ws.Messages)-1]
	require.Equal(t, domain.RefBriefConfirm, open.RefType)
	ws = f.post(t, p.ID, "はい", "")
	require.Len(t, ws.Updated, 1)
	assert.Equal(t, open.ID, ws.Updated[0].ID)
	assert.False(t, ws.Updated[0].Open)
}
