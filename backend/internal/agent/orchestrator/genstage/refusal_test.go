package genstage_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/generation"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/modelroute"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/genapprovalrepo"
)

// noModel is the refusal of the preflight of the Generation Orchestrator (#1113) as the start returns it.
func noModel(key string) error {
	return fmt.Errorf("start the generation: %w", &modelroute.NoUsableError{Media: "image", Key: key})
}

// told counts the notices of the Gate about a start that did not happen.
func (r *rig) told() (n int) {
	for _, text := range r.chat.notices() {
		if strings.Contains(text, "を自動では始められませんでした") {
			n++
		}
	}
	return n
}

// The announcement is made for a start that was made. A start that ends busy or gone (the claim is given back) is looked at again later; it must not announce each time, only when a Shot is started.
func TestAutoStart_NoAnnouncementForLooksWhoseEveryStartEndsBusy(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	for _, s := range r.shotIDs(id) {
		r.gen.errs[s] = generation.ErrShotBusy
	}

	r.handle(id)
	r.handle(id)
	r.handle(id)

	assert.Zero(t, r.announcements(), "nothing was started: nothing is announced")
	assert.Empty(t, r.gen.started())

	clear(r.gen.errs)
	r.handle(id)
	r.handle(id)

	assert.Equal(t, 3, r.gen.count(genapprovalrepo.StageReferencePack))
	assert.Equal(t, 1, r.announcements(), "one announcement, for the start that was made")
}

// withKeyframe makes the Shot one that needs no Reference Pack (a Keyframe is in its spec): only its video is generated.
func (r *rig) withKeyframe(shotID string) {
	r.t.Helper()
	spec, _ := json.Marshal(map[string]any{"references": []domain.Reference{{Kind: domain.RefKeyframe, AssetID: "a", Prompt: "p"}}})
	require.NoError(r.t, r.db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE shot SET shot_spec = ? WHERE id = ?`, string(spec), shotID)
		return err
	}))
}

// A model that cannot be used is the state of the models and not of the Shot: the first start it refuses is marked and the cause told once, but the
// round goes on, because another Shot may need another model. The Shots refused for the same cause are not marked (they are given their claim back), and
// the later looks do not post the cause again or mark another Shot while it is the same. When the user fixes it the Shots that were left go on; the Shot
// that failed stays the user's. The notice says what really lifts the wait: a message in the chat (a look at the Project), not the key itself.
func TestAutoStart_NoUsableModelIsToldOncePerCauseAndTheRoundGoesOn(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	for _, s := range shots {
		r.gen.errs[s] = noModel("reference_pack")
	}

	r.handle(id)

	assert.Equal(t, shots, r.gen.attempts(), "the round goes on past the refusal: every Shot is tried")
	assert.Equal(t, 1, r.told(), "one notice for the cause")
	launches, err := r.launches.Launches(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, genapprovalrepo.OutcomeFailed, launches[shots[0]][genapprovalrepo.StageReferencePack].Outcome, "the first Shot of the cause is marked failed")
	assert.NotContains(t, launches, shots[1], "a Shot refused for the same cause is released, not marked")
	assert.NotContains(t, launches, shots[2])
	notice := r.chat.notices()[0]
	assert.Contains(t, notice, "Shot 1 の Reference Pack を自動では始められませんでした")
	assert.Contains(t, notice, "API キーを登録するか割り当てを直したあと、チャットに何かメッセージを送ると、残りの Shot を続けます")
	assert.NotContains(t, notice, "次の確認で自動で続けます", "saving a key does not look at the Project by itself")

	r.handle(id)
	r.handle(id)
	assert.Equal(t, 1, r.told(), "the same cause is not told again by the looks that follow")

	for _, s := range shots {
		delete(r.gen.errs, s) // the user registered a key
	}
	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[1]}, {genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started(),
		"the Shots that were left go on; the one that failed stays the user's")
}

// One missing model does not stop a Shot that needs another: of three Shots, two need a Reference Pack (the image model is missing) and one only a video
// (it has a Keyframe). The video is started; the first pack is marked and told, the second is released, and the looks after it, whose round meets the
// refusal again and starts nothing new, say nothing more.
func TestAutoStart_AMissingImageModelDoesNotStopAVideoOnlyShot(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.withKeyframe(shots[1])
	r.gen.errs[shots[0]] = noModel("reference_pack")
	r.gen.errs[shots[2]] = noModel("reference_pack")

	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageGenerate, shots[1]}}, r.gen.started(), "the video-only Shot runs")
	assert.Equal(t, 1, r.told())
	launches, err := r.launches.Launches(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, genapprovalrepo.OutcomeFailed, launches[shots[0]][genapprovalrepo.StageReferencePack].Outcome)
	assert.NotContains(t, launches, shots[2], "released: the cause is the same as Shot 1's")

	r.handle(id)
	assert.Equal(t, 1, r.told(), "a start in the round does not end the silence about a cause the round met again")
}

// Each cause is told once, even when one round meets two (another purpose is another cause). A cause that comes back after a round that started
// something and met no refusal is told again (the user fixed it and it broke again).
func TestAutoStart_NoUsableModelIsToldAgainForAnotherCauseOrAfterAStartSucceeded(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.gen.errs[shots[0]] = noModel("reference_pack")
	r.gen.errs[shots[1]] = noModel("shot_still") // another purpose: another cause
	r.gen.errs[shots[2]] = noModel("reference_pack")

	r.handle(id)

	assert.Equal(t, 2, r.told(), "Shot 2 is refused for another cause: it is told in the same round")
	assert.Contains(t, r.chat.notices()[len(r.chat.notices())-1], "shot_still")
	r.handle(id)
	assert.Equal(t, 2, r.told(), "neither is told again")

	r.gen.errs[shots[2]] = nil // a start succeeds, with no refusal in the round: the silence about the causes ends
	r.handle(id)
	require.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started())

	scenes, err := r.plans.ListScenes(context.Background(), id)
	require.NoError(t, err)
	added, err := r.plans.CreateShot(context.Background(), domain.Shot{SceneID: scenes[0].ID, Objective: "追加", Duration: 4})
	require.NoError(t, err)
	r.gen.errs[added.ID] = noModel("shot_still") // the cause told before comes back after a start succeeded
	r.handle(id)

	assert.Equal(t, 3, r.told(), "told again")
}
