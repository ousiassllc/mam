package voicerefsvc_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
)

const trainedID = "0190c7f0-1111-7000-8000-000000000001"

// trained writes the two weights of a trained voice as the training does and returns it.
func (f *fixture) trained(id string) voiceref.Finetune {
	f.t.Helper()
	ft := voiceref.Finetune{ID: id, GPTSizeBytes: 5, SoVITSSizeBytes: 7, Version: voiceref.FinetuneVersion, GPTEpochs: 15, SoVITSEpochs: 8, Clips: 6, DataSeconds: 60}
	dir := filepath.Join(f.assets, filepath.FromSlash(ft.Dir()))
	require.NoError(f.t, os.MkdirAll(dir, 0o700))
	require.NoError(f.t, os.WriteFile(filepath.Join(dir, voiceref.GPTFileName), []byte("gpt-w"), 0o600))
	require.NoError(f.t, os.WriteFile(filepath.Join(dir, voiceref.SoVITSFileName), []byte("sovits-"), 0o600))
	return ft
}

func (f *fixture) withReference() {
	f.t.Helper()
	_, err := f.svc.SetForProject(context.Background(), "p1", "c1", voiceInput(wav(100), "おはよう"))
	require.NoError(f.t, err)
}

func TestBindFinetune_MakesTheWeightsTheTrainedVoiceOfTheCharacterAndTheLookupFindsThem(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.withReference()
	ft := f.trained(trainedID)

	require.NoError(t, f.svc.BindFinetune(ctx, voicerefsvc.ForProject("p1"), "c1", ft))

	v, err := f.svc.ForSpeaker(ctx, "p1", "", "ユイ")
	require.NoError(t, err)
	require.NotNil(t, v.Trained)
	assert.Equal(t, trainedID, v.Trained.ID)
	assert.NotEmpty(t, v.Path, "the recording is still what the synthesis reads for the prosody")
	assert.Equal(t, "おはよう", v.Reference.Text)
	assert.Equal(t, f.svc.WeightsRoot(), filepath.Dir(f.svc.WeightsDir(trainedID)))
	assert.Equal(t, filepath.Join(f.assets, "voicetrain"), f.svc.WeightsRoot())
}

func TestBindFinetune_RefusesWhatItCannotStandBehind(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	owner := voicerefsvc.ForProject("p1")
	ft := f.trained(trainedID)

	assert.ErrorIs(t, f.svc.BindFinetune(ctx, owner, "c1", ft), voicerefsvc.ErrNoReference, "a Character without a reference voice")
	f.withReference()
	assert.ErrorIs(t, f.svc.BindFinetune(ctx, owner, "ghost", ft), voicerefsvc.ErrNotFound)

	for name, bad := range map[string]voiceref.Finetune{
		"an id that is a path":        {ID: "../x", GPTSizeBytes: 5, SoVITSSizeBytes: 7},
		"an empty id":                 {GPTSizeBytes: 5, SoVITSSizeBytes: 7},
		"no sizes":                    {ID: trainedID},
		"another size than the files": {ID: trainedID, GPTSizeBytes: 6, SoVITSSizeBytes: 7},
		"weights that are not there":  {ID: "0190c7f0-2222-7000-8000-000000000002", GPTSizeBytes: 5, SoVITSSizeBytes: 7},
	} {
		assert.Error(t, f.svc.BindFinetune(ctx, owner, "c1", bad), name)
	}
	ref, err := f.svc.Reference(ctx, owner, "c1")
	require.NoError(t, err)
	assert.Nil(t, ref.Finetune, "nothing was bound")
}

func TestForSpeaker_AMissingOrDamagedWeightFileStopsTheVoiceInsteadOfSpeakingWithAnotherOne(t *testing.T) {
	ctx := context.Background()
	for name, damage := range map[string]func(dir string){
		"a missing GPT file":    func(dir string) { require.NoError(t, os.Remove(filepath.Join(dir, voiceref.GPTFileName))) },
		"a missing SoVITS file": func(dir string) { require.NoError(t, os.Remove(filepath.Join(dir, voiceref.SoVITSFileName))) },
		"a file of another size": func(dir string) {
			require.NoError(t, os.WriteFile(filepath.Join(dir, voiceref.GPTFileName), []byte("short"+"er"), 0o600))
		},
		"a removed directory": func(dir string) { require.NoError(t, os.RemoveAll(dir)) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.withReference()
			ft := f.trained(trainedID)
			require.NoError(t, f.svc.BindFinetune(ctx, voicerefsvc.ForProject("p1"), "c1", ft))
			damage(f.svc.WeightsDir(trainedID))

			_, err := f.svc.ForSpeaker(ctx, "p1", "", "ユイ")

			assert.ErrorIs(t, err, voicerefsvc.ErrNoWeights, "it is not a silent fall back to the zero-shot clone")
			assert.NotErrorIs(t, err, voicerefsvc.ErrFileMissing)
			// Taking the voice off is the way back to the zero-shot clone, and it works with the files gone.
			require.NoError(t, f.svc.RemoveFinetune(ctx, voicerefsvc.ForProject("p1"), "c1"))
			v, err := f.svc.ForSpeaker(ctx, "p1", "", "ユイ")
			require.NoError(t, err)
			assert.Nil(t, v.Trained)
		})
	}
}

func TestRemoveFinetune_RemovesTheWeightsAndKeepsTheRecording(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.withReference()
	owner := voicerefsvc.ForProject("p1")
	require.NoError(t, f.svc.RemoveFinetune(ctx, owner, "c1"), "nothing to remove is not an error")
	require.NoError(t, f.svc.BindFinetune(ctx, owner, "c1", f.trained(trainedID)))

	require.NoError(t, f.svc.RemoveFinetune(ctx, owner, "c1"))

	assert.NoDirExists(t, f.svc.WeightsDir(trainedID))
	v, err := f.svc.ForSpeaker(ctx, "p1", "", "ユイ")
	require.NoError(t, err)
	assert.Nil(t, v.Trained)
	assert.FileExists(t, v.Path)
}

func TestBindFinetune_ANewTrainingReplacesTheOldWeightsAndANewRecordingKeepsTheTrainedVoice(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.withReference()
	owner := voicerefsvc.ForProject("p1")
	first := f.trained(trainedID)
	require.NoError(t, f.svc.BindFinetune(ctx, owner, "c1", first))

	// A new recording (the same speaker, a better take) keeps the trained voice.
	_, err := f.svc.SetForProject(ctx, "p1", "c1", voiceInput(wav(120), "こんばんは"))
	require.NoError(t, err)
	ref, err := f.svc.Reference(ctx, owner, "c1")
	require.NoError(t, err)
	require.NotNil(t, ref.Finetune)
	assert.Equal(t, trainedID, ref.Finetune.ID)
	assert.Equal(t, "こんばんは", ref.Text)

	// A second training replaces it: the first weights are removed.
	second := f.trained("0190c7f0-3333-7000-8000-000000000003")
	require.NoError(t, f.svc.BindFinetune(ctx, owner, "c1", second))
	assert.NoDirExists(t, f.svc.WeightsDir(first.ID))
	assert.DirExists(t, f.svc.WeightsDir(second.ID))
}

func TestRemoveForProject_RemovesTheTrainedVoiceWithTheReferenceVoice(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.withReference()
	require.NoError(t, f.svc.BindFinetune(ctx, voicerefsvc.ForProject("p1"), "c1", f.trained(trainedID)))

	require.NoError(t, f.svc.RemoveForProject(ctx, "p1", "c1"))

	assert.NoDirExists(t, f.svc.WeightsDir(trainedID))
	assert.Empty(t, f.files(), "no file of the voice is left")
}

func TestCleanFinetunes_RemovesWhatNoCharacterUsesAndKeepsTheRest(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.withReference()
	require.NoError(t, f.svc.BindFinetune(ctx, voicerefsvc.ForProject("p1"), "c1", f.trained(trainedID)))
	orphan := f.trained("0190c7f0-4444-7000-8000-000000000004")
	staging := filepath.Join(f.assets, "voicetrain", ".tmp-0190c7f0-5555-7000-8000-000000000005")
	require.NoError(t, os.MkdirAll(staging, 0o700))
	draft := filepath.Join(f.assets, "voicetrain", "_draft", "project-p1", "c1")
	require.NoError(t, os.MkdirAll(draft, 0o700))

	f.svc.CleanFinetunes(ctx)

	assert.DirExists(t, f.svc.WeightsDir(trainedID), "a Character uses it")
	assert.NoDirExists(t, f.svc.WeightsDir(orphan.ID))
	assert.NoDirExists(t, staging, "what an interrupted keep left")
	assert.DirExists(t, draft, "the drafts of the clips are not trained voices")
}

func TestBundle_TheExportLeavesTheTrainedVoiceOutAndTheImportSpeaksWithTheRecording(t *testing.T) {
	ctx := context.Background()
	src := newApp(t)
	src.exec(t, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`)
	src.exec(t, `INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'ユイ')`)
	_, err := src.svc.VoiceRef.SetForProject(ctx, "p1", "c1", voiceInput(wav(100), "元の声"))
	require.NoError(t, err)
	ft := voiceref.Finetune{ID: trainedID, GPTSizeBytes: 5, SoVITSSizeBytes: 7, Version: voiceref.FinetuneVersion}
	dir := filepath.Join(src.assets, filepath.FromSlash(ft.Dir()))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, voiceref.GPTFileName), []byte("gpt-w"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, voiceref.SoVITSFileName), []byte("sovits-"), 0o600))
	require.NoError(t, src.svc.VoiceRef.BindFinetune(ctx, voicerefsvc.ForProject("p1"), "c1", ft))

	dst := newApp(t)
	move(t, src, dst, "p1")

	voice, err := dst.svc.VoiceRef.ForSpeaker(ctx, "p1", "", "ユイ")
	require.NoError(t, err, "the import is fine: the Character speaks with the zero-shot clone of its recording")
	assert.Nil(t, voice.Trained)
	assert.Nil(t, voice.Reference.Finetune)
	assert.Equal(t, "元の声", voice.Reference.Text)
	assert.NoDirExists(t, filepath.Join(dst.assets, voiceref.FinetuneDir), "no weights came with the bundle")
	// The exporter keeps its own trained voice.
	kept, err := src.svc.VoiceRef.ForSpeaker(ctx, "p1", "", "ユイ")
	require.NoError(t, err)
	require.NotNil(t, kept.Trained)
}
