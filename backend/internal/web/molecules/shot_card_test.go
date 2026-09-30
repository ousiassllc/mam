package molecules_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
)

func TestShotCard_OnlyOneElementFollowsTheProgressSocket(t *testing.T) {
	cases := map[domain.ShotState]struct{ player, cardSocket bool }{
		domain.ShotGenerating: {true, false}, // the player follows it, shot.ts waits for the player
		domain.ShotJudging:    {true, false},
		domain.ShotRepairing:  {false, true}, // no player on the card: shot.ts follows it
		domain.ShotEditing:    {false, false},
	}
	for state, want := range cases {
		html := render(t, molecules.ShotCard(molecules.ShotCardData{ProjectID: "p", Shot: domain.Shot{ID: "s1", SceneID: "sc", State: state}}))
		wantSockets := 0
		if want.player || want.cardSocket {
			wantSockets = 1
		}
		assert.Equal(t, wantSockets, strings.Count(html, "/ws/shots/s1"), string(state))
		assert.Equal(t, want.player, strings.Contains(html, `<mam-video-player class="mt-2 block" ws-url="/ws/shots/s1"`), string(state))
		assert.Equal(t, want.cardSocket, strings.Contains(html, `data-ws-url="/ws/shots/s1"`), string(state))
		assert.Equal(t, wantSockets == 1, strings.Contains(html, "data-shot-progress"), string(state))
	}
}
