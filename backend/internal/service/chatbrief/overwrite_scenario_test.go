package chatbrief_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

// Replying to the open contradiction card with a phrasing nobody listed never decides the Subject: the Subject stays, the contradiction
// stays open and the card is asked again (#1114, the default is to keep what the user decided).
func TestScenario_AnUnlistedPhrasingOnTheContradictionCardKeepsTheSubject(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, firstRequest)
	f.post(t, p.ID, "ロボットを主人公にした動画も作りたい", "")
	q := f.cards(t, p.ID)[1]
	require.Equal(t, string(domain.BriefSubject), q.RefID)

	replyTo := q.ID
	for _, text := range []string{"動画を作りたいですけど", "動画をつくりたいですw", "うーん、考え中です"} {
		f.post(t, p.ID, text, replyTo)
		replyTo = f.lastMessage(t, p.ID).ID // the question asked again
		b := f.brief(t, p.ID)
		assert.Equal(t, protagonist, b.Subject, text)
		assert.Equal(t, "ロボット", b.ProposedSubject, "still open: %s", text)
	}
}

// A plain change in chat still replaces the Subject, whatever finished verb it ends in, and a noun outside the shape is asked about, not dropped.
func TestScenario_APlainChangeInChatStillWorks(t *testing.T) {
	f := newPipelineFixture(t)
	p := f.create(t, firstRequest)
	f.post(t, p.ID, "主人公を猫に変えて", "")
	assert.Equal(t, "猫", f.brief(t, p.ID).Subject)
	assert.Empty(t, f.brief(t, p.ID).ProposedSubject)

	f.post(t, p.ID, "主人公を空飛ぶ犬に変更して", "")
	b := f.brief(t, p.ID)
	assert.Equal(t, "猫", b.Subject)
	assert.Equal(t, "空飛ぶ犬", b.ProposedSubject, "asked about, with the card")
}
