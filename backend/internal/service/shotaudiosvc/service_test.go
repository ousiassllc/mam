package shotaudiosvc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/shotaudio"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc"
	"github.com/ousiassllc/mam/backend/internal/service/shotaudiosvc/shotaudiotest"
)

var wav = shotaudiotest.WAV

func TestSet_TiesAndClearsTheAudioAndKeepsTheRestOfTheSpec(t *testing.T) {
	f := shotaudiotest.New(t)
	f.Asset("p1", "a1", domain.AssetAudio, wav(3))

	v, err := f.Svc.Set(context.Background(), "p1", "s1", "a1")

	require.NoError(t, err)
	require.NotNil(t, v.Tie)
	assert.Equal(t, "a1", v.Tie.Asset.ID)
	assert.InDelta(t, 3, v.Tie.Seconds, 0.01)
	assert.Empty(t, v.Tie.Problem)
	assert.Equal(t, 1, v.Scene)
	assert.Equal(t, 1, v.Number)
	assert.Equal(t, "a1", shotaudio.ID(f.Spec("s1")))
	assert.Contains(t, string(f.Spec("s1")), `"description":"d"`, "the storyboard stays")
	require.Len(t, f.Recorded, 1)
	assert.Equal(t, domain.InterventionEditShotSpec, f.Recorded[0].Kind)

	got, err := f.Svc.Get(context.Background(), "p1", "s1")
	require.NoError(t, err)
	assert.Equal(t, v.Tie, got.Tie)
	require.Len(t, got.Choices, 1, "the audio assets of the Project can be picked")

	cleared, err := f.Svc.Set(context.Background(), "p1", "s1", "")
	require.NoError(t, err)
	assert.Nil(t, cleared.Tie)
	assert.Empty(t, shotaudio.ID(f.Spec("s1")))
	assert.Len(t, f.Recorded, 2, "taking the audio off is an edit too")

	_, err = f.Svc.Set(context.Background(), "p1", "s1", "")
	require.NoError(t, err, "clearing a Shot without audio is not an error")
	assert.Len(t, f.Recorded, 2, "and it changes nothing, so it is not noted")
}

func TestSet_RefusesWhatIsNotAnAudioAssetOfTheShotsProject(t *testing.T) {
	f := shotaudiotest.New(t)
	f.Asset("p1", "img", domain.AssetImage, []byte("png"))
	f.Asset("p2", "foreign", domain.AssetAudio, wav(2))
	ctx := context.Background()

	_, err := f.Svc.Set(ctx, "p1", "s1", "img")
	var invalid *shotaudiosvc.InvalidError
	require.ErrorAs(t, err, &invalid, "an image is no audio")
	assert.Equal(t, "asset_id", invalid.Field)

	_, err = f.Svc.Set(ctx, "p1", "s1", "foreign")
	assert.ErrorIs(t, err, shotaudiosvc.ErrNotFound, "an asset of another Project is not found")

	_, err = f.Svc.Set(ctx, "p1", "s1", "nothing")
	assert.ErrorIs(t, err, shotaudiosvc.ErrNotFound)

	_, err = f.Svc.Set(ctx, "p1", "other", "foreign")
	assert.ErrorIs(t, err, shotaudiosvc.ErrNotFound, "a Shot of another Project is not found")
	_, err = f.Svc.Set(ctx, "p2", "s1", "foreign")
	assert.ErrorIs(t, err, shotaudiosvc.ErrNotFound)
	_, err = f.Svc.Set(ctx, "p1", "ghost", "")
	assert.ErrorIs(t, err, shotaudiosvc.ErrNotFound)

	assert.Empty(t, shotaudio.ID(f.Spec("s1")), "nothing was written")
	assert.Empty(t, f.Recorded)
}

func TestSet_ARunningOrLockedShotKeepsItsAudio(t *testing.T) {
	f := shotaudiotest.New(t)
	f.Asset("p1", "a1", domain.AssetAudio, wav(2))
	f.Asset("p1", "a2", domain.AssetAudio, wav(2))
	ctx := context.Background()
	_, err := f.Svc.Set(ctx, "p1", "s1", "a1")
	require.NoError(t, err)

	f.Exec(`UPDATE shot SET state = 'GENERATING' WHERE id = 's1'`)
	_, err = f.Svc.Set(ctx, "p1", "s1", "a2")
	assert.ErrorIs(t, err, shotaudiosvc.ErrBusy, "the generation reads the audio: it is not changed meanwhile")
	_, err = f.Svc.Set(ctx, "p1", "s1", "")
	assert.ErrorIs(t, err, shotaudiosvc.ErrBusy)
	f.Exec(`UPDATE shot SET state = 'PLANNING' WHERE id = 's1'`)

	f.Exec(`INSERT INTO lock (id, project_id, target_type, target_id, parameter_name, mode) VALUES ('l1', 'p1', 'shot', 's1', 'prompt', 'lock')`)
	for _, id := range []string{"a2", ""} {
		_, err = f.Svc.Set(ctx, "p1", "s1", id)
		var locked *shotaudiosvc.LockedError
		require.ErrorAs(t, err, &locked, "asset %q", id)
		assert.Equal(t, []domain.LockParameter{domain.ParamPrompt}, locked.Parameters)
		assert.ErrorIs(t, err, repository.ErrLocked)
	}
	assert.Equal(t, "a1", shotaudio.ID(f.Spec("s1")))
	v, err := f.Svc.Get(ctx, "p1", "s1")
	require.NoError(t, err)
	assert.True(t, v.Locked)

	f.Exec(`UPDATE lock SET mode = 'manual' WHERE id = 'l1'`)
	_, err = f.Svc.Set(ctx, "p1", "s1", "a2")
	require.NoError(t, err, "only a LOCK stops the change")
}

// The Shot screen has to show a Shot whose audio went away so that the audio can be changed: it is a Problem of the Tie, not an error.
func TestGet_AnAudioThatCannotBeUsedIsAProblemOfTheTie(t *testing.T) {
	f := shotaudiotest.New(t)
	f.Limit = 10
	ctx := context.Background()
	f.Asset("p1", "x1", domain.AssetImage, []byte("png"))
	f.Asset("p1", "x2", domain.AssetAudio, nil)
	f.Asset("p1", "x3", domain.AssetAudio, []byte("not audio at all"))
	f.Asset("p1", "x4", domain.AssetAudio, wav(12))
	for id, problem := range map[string]string{
		"ghost": "見つかりません", // the asset row is gone
		"x1":    "音声ファイルではありません",
		"x2":    "読めません", // the file is gone
		"x3":    "長さを読めません",
		"x4":    "長すぎます",
	} {
		f.Exec(`UPDATE shot SET shot_spec = ? WHERE id = 's1'`, `{"audio":{"asset_id":"`+id+`"}}`)

		v, err := f.Svc.Get(ctx, "p1", "s1")

		require.NoError(t, err, id)
		require.NotNil(t, v.Tie, id)
		assert.Contains(t, v.Tie.Problem, problem, id)
	}

	_, err := f.Svc.Set(ctx, "p1", "s1", "")
	require.NoError(t, err, "a dangling tie can always be taken off")
}

func TestGet_ASpecWrittenBeforeTheKeyHasNoAudio(t *testing.T) {
	f := shotaudiotest.New(t)
	f.Asset("p1", "a1", domain.AssetAudio, wav(2))
	f.Asset("p1", "a0", domain.AssetAudio, wav(2))
	f.Asset("p1", "img", domain.AssetImage, []byte("png"))

	v, err := f.Svc.Get(context.Background(), "p1", "s1")

	require.NoError(t, err)
	assert.Nil(t, v.Tie)
	assert.False(t, v.Locked)
	var ids []string
	for _, a := range v.Choices {
		ids = append(ids, a.ID)
	}
	assert.ElementsMatch(t, []string{"a0", "a1"}, ids, "only audio assets are choices")
}
