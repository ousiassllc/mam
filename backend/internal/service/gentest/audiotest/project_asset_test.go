package audiotest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// The audio assets of a Project (Issue #1153, spec/architecture/gentest/gentest.md "取得元"): the sound of a batch can be an asset of type audio.

// seedAsset writes a file of the Project's assets directory and registers it as an asset of the type, the way an attachment of the chat is stored.
func seedAsset(t *testing.T, r *gentesttest.Rig, project string, typ domain.AssetType, ext, mime string, data []byte) string {
	t.Helper()
	id := domain.NewID()
	rel := filepath.Join(project, id+ext)
	require.NoError(t, os.MkdirAll(filepath.Join(r.AssetsDir, project), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(r.AssetsDir, rel), data, 0o600))
	require.NoError(t, r.Repo.AddProjectAsset(context.Background(), domain.Asset{
		ID: id, ProjectID: project, Type: typ, FilePath: filepath.ToSlash(rel), MimeType: mime, SizeBytes: int64(len(data)),
	}))
	return id
}

func fromProject(project, id string) gentest.RefInput {
	return gentest.RefInput{Source: gentest.FromProjectAsset, ProjectID: project, ID: id, Role: gentest.RoleAudio}
}

// Every kind of audio asset is the sound of a batch: the model gets the bytes with the type of the asset, and the batch keeps a copy of the file and says it came
// from the Project's asset.
func TestTheSoundOfABatchCanBeAnAudioAssetOfAProject(t *testing.T) {
	for _, c := range []struct {
		name, ext, mime string
		data            []byte
	}{
		{"wav", ".wav", audiofile.WAV, audiofiletest.WAV(16000, 32000)},
		{"mp3", ".mp3", audiofile.MP3, audiofiletest.MP3(true, 40)},
		{"flac", ".flac", audiofile.FLAC, audiofiletest.FLAC(44100, 44100)},
		{"ogg", ".ogg", audiofile.OGG, audiofiletest.OGGVorbis(44100, 44100)},
		{"m4a", ".m4a", audiofile.M4A, audiofiletest.M4A(0, 44100, 44100, true)},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := gentesttest.New(t, gentesttest.WithInputModels())
			ctx := context.Background()
			project := r.NewProject()
			assetID := seedAsset(t, r, project, domain.AssetAudio, c.ext, c.mime, c.data)

			ref, err := r.Confirmed(videoReq([]string{s2v}, picture(t, r, "reference"), fromProject(project, assetID)))
			require.NoError(t, err)
			st := r.Wait(ref.BatchID)
			require.Equal(t, gentest.StateSucceeded, st.State, "%+v", st.Results)

			v := r.Fake.CallsOf(gentesttest.S2VModel)[0].Video
			require.NotNil(t, v.Audio)
			assert.Equal(t, c.data, v.Audio.Data)
			assert.Equal(t, c.mime, v.Audio.MimeType)

			inputs, err := r.Repo.Inputs(ctx, ref.BatchID)
			require.NoError(t, err)
			require.Len(t, inputs, 2)
			sound := inputs[1]
			assert.Equal(t, "audio", sound.Role)
			assert.Equal(t, c.mime, sound.MimeType)
			assert.Equal(t, c.ext, filepath.Ext(sound.FilePath))
			assert.JSONEq(t, `{"kind":"project_asset","id":"`+assetID+`","project_id":"`+project+`"}`, string(sound.Origin))
			f, err := r.Svc.InputFile(ctx, sound.ID)
			require.NoError(t, err)
			copied, err := os.ReadFile(f.Path)
			require.NoError(t, err)
			assert.Equal(t, c.data, copied, "the batch holds its own copy")
			_, err = os.Stat(filepath.Join(r.AssetsDir, project, assetID+c.ext))
			assert.NoError(t, err, "the asset of the Project stays")
		})
	}
}

// What is not an audio asset of this Project is refused where the sound is taken, and a picture is never taken for a sound nor a sound for a picture.
func TestOnlyAnAudioAssetOfTheProjectIsTheSoundAndNeverAPicture(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	ctx := context.Background()
	project, other := r.NewProject(), r.NewProject()
	wav := audiofiletest.WAV(16000, 16000)
	sound := seedAsset(t, r, project, domain.AssetAudio, ".wav", audiofile.WAV, wav)
	image := seedAsset(t, r, project, domain.AssetImage, ".png", "image/png", gentesttest.PNG)
	reference := seedAsset(t, r, project, domain.AssetReference, ".png", "image/png", gentesttest.PNG)
	video := seedAsset(t, r, project, domain.AssetVideo, ".mp4", "video/mp4", []byte("\x00\x00\x00\x18ftypisom"))
	lying := seedAsset(t, r, project, domain.AssetAudio, ".wav", audiofile.WAV, gentesttest.PNG) // the row says WAV, the bytes are a picture
	cut := seedAsset(t, r, project, domain.AssetAudio, ".wav", audiofile.WAV, nil)

	asSound := func(ref gentest.RefInput) gentest.Request {
		return videoReq([]string{s2v}, picture(t, r, "reference"), ref)
	}
	for name, c := range map[string]struct {
		ref  gentest.RefInput
		want string
	}{
		"an image asset":          {fromProject(project, image), "音声ファイルではありません"},
		"a reference asset":       {fromProject(project, reference), "音声ファイルではありません"},
		"a video asset":           {fromProject(project, video), "音声ファイルではありません"},
		"an asset of another one": {fromProject(other, sound), "見つかりません"},
		"an unknown asset":        {fromProject(project, "nope"), "見つかりません"},
		"bytes of another kind":   {fromProject(project, lying), "ではありません"},
		"an empty file":           {fromProject(project, cut), "ではありません"},
		"a Library entry":         {gentest.RefInput{Source: gentest.FromReferenceItem, ProjectID: project, ID: sound, Role: gentest.RoleAudio}, "upload・batch_input・project_asset"},
		"no project":              {gentest.RefInput{Source: gentest.FromProjectAsset, ID: sound, Role: gentest.RoleAudio}, "project_id が要ります"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := r.Svc.Estimate(ctx, asSound(c.ref))
			assert.Contains(t, invalid(t, err), c.want)
			_, err = r.Svc.Run(ctx, asSound(c.ref), gentest.Confirmation{})
			assert.Error(t, err, "Run refuses it too")
		})
	}

	// a sound where a picture is wanted: the refusal says it is a sound
	// (the model gets the first frame it needs, so that the sound is the only thing wrong)
	for _, role := range []string{"reference", "last_frame"} {
		req := videoReq([]string{string(gentesttest.I2VModel)}, picture(t, r, "first_frame"), gentest.RefInput{Source: gentest.FromProjectAsset, ProjectID: project, ID: sound, Role: role})
		_, err := r.Svc.Estimate(ctx, req)
		assert.Contains(t, invalid(t, err), "音声ファイルです", role)
	}
	// while a picture of the Project is a fine first frame
	_, err := r.Svc.Estimate(ctx, videoReq([]string{string(gentesttest.I2VModel)}, gentest.RefInput{Source: gentest.FromProjectAsset, ProjectID: project, ID: image, Role: "first_frame"}))
	require.NoError(t, err)

	assert.Empty(t, r.Fake.Calls(), "nothing was sent")
	var batches int
	require.NoError(t, r.DB.Reader().QueryRow(`SELECT COUNT(*) FROM gentest_batch`).Scan(&batches))
	assert.Zero(t, batches, "no batch was made")
}

// The Project's audio assets are listed for the picker, newest first, with nothing else in the list; the picture list has no sound in it.
func TestProjectAudiosListsOnlyTheAudioAssetsOfTheProject(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithInputModels())
	ctx := context.Background()
	project := r.NewProject()
	first := seedAsset(t, r, project, domain.AssetAudio, ".wav", audiofile.WAV, audiofiletest.WAV(16000, 16000))
	seedAsset(t, r, project, domain.AssetImage, ".png", "image/png", gentesttest.PNG)
	second := seedAsset(t, r, project, domain.AssetAudio, ".mp3", audiofile.MP3, audiofiletest.MP3(true, 20))
	seedAsset(t, r, r.NewProject(), domain.AssetAudio, ".wav", audiofile.WAV, audiofiletest.WAV(16000, 16000))

	list, err := r.Svc.ProjectAudios(ctx, project)
	require.NoError(t, err)
	var ids []string
	for _, a := range list {
		ids = append(ids, a.AssetID)
		assert.True(t, audiofile.Is(a.MimeType))
	}
	assert.ElementsMatch(t, []string{first, second}, ids)

	images, err := r.Svc.ProjectImages(ctx, project)
	require.NoError(t, err)
	assert.Len(t, images, 1, "a sound is no picture of the picker")
	_, err = r.Svc.ProjectAudios(ctx, "nope")
	assert.ErrorIs(t, err, gterr.ErrNotFound)
}
