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
