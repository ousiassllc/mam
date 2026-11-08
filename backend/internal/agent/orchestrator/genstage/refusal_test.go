package genstage_test

import (
	"context"
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

// The announcement is made for a start that was made. A start that ends busy or gone (the claim is given back) is looked at again later; it must not
// announce each time, only when a Shot is started.
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

// A model that cannot be used is the state of the models and not of the Shot: the first start it refuses marks only that Shot, stops the round and tells
// the cause once. The Shots after it are not tried (they stay unclaimed), and the later looks do not post it again or mark another Shot while the cause
// is the same. When a start succeeds again the Shots that were left go on; the Shot that failed stays the user's.
func TestAutoStart_NoUsableModelStopsTheRoundAndIsToldOncePerCause(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	for _, s := range shots {
		r.gen.errs[s] = noModel("reference_pack")
	}

	r.handle(id)

	assert.Equal(t, []string{shots[0]}, r.gen.attempts(), "the round stops at the first refusal for a model")
	assert.Equal(t, 1, r.told(), "one notice for the round")
	launches, err := r.launches.Launches(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, genapprovalrepo.OutcomeFailed, launches[shots[0]][genapprovalrepo.StageReferencePack].Outcome, "the Shot that was tried is marked failed")
	assert.NotContains(t, launches, shots[1])
	assert.NotContains(t, launches, shots[2])
	notice := r.chat.notices()[0]
	assert.Contains(t, notice, "Shot 1 の Reference Pack を自動では始められませんでした")
	assert.Contains(t, notice, "画像（用途: reference_pack）に使えるモデルがありません")
	assert.Contains(t, notice, "ほかの Shot はまだ始めていません")

	r.handle(id)
	r.handle(id)
	assert.Equal(t, 1, r.told(), "the same cause is not told again by the looks that follow")
	launches, err = r.launches.Launches(context.Background(), id)
	require.NoError(t, err)
	assert.NotContains(t, launches, shots[1], "a Shot after the refused one is not marked while the cause is the same")
	assert.NotContains(t, launches, shots[2])

	for _, s := range shots {
		delete(r.gen.errs, s) // the user registered a key
	}
	r.handle(id)

	assert.Equal(t, []call{{genapprovalrepo.StageReferencePack, shots[1]}, {genapprovalrepo.StageReferencePack, shots[2]}}, r.gen.started(),
		"the Shots that were left go on; the one that failed stays the user's")
	assert.Equal(t, 1, r.told())
}

// Another cause is told again, and so is the same cause after a start succeeded in between (the user fixed it and it broke again).
func TestAutoStart_NoUsableModelIsToldAgainForAnotherCauseOrAfterAStartSucceeded(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	id := r.planned(domain.ConfirmFullAuto)
	shots := r.shotIDs(id)
	r.gen.errs[shots[0]] = noModel("reference_pack")
	r.gen.errs[shots[1]] = noModel("shot_still") // another purpose: another cause
	r.gen.errs[shots[2]] = noModel("reference_pack")

	r.handle(id)
	require.Equal(t, 1, r.told())
	r.handle(id)

	assert.Equal(t, 2, r.told(), "Shot 2 is refused for another cause: it is told")
	assert.Contains(t, r.chat.notices()[len(r.chat.notices())-1], "shot_still")

	r.gen.errs[shots[2]] = nil // a start succeeds: the silence about the cause ends
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
