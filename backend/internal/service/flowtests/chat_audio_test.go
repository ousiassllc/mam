package flowtests_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/audiofile"
	"github.com/ousiassllc/mam/backend/internal/audiofile/audiofiletest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// audioFiles is one file of every kind a Project takes as an audio asset (Issue #1153), with the name it is attached under and the type it is stored as.
func audioFiles() []struct {
	name, mime string
	data       []byte
} {
	return []struct {
		name, mime string
		data       []byte
	}{
		{"voice.wav", audiofile.WAV, audiofiletest.WAV(16000, 16000)},
		{"voice.MP3", audiofile.MP3, audiofiletest.MP3(true, 20)},
		{"voice.flac", audiofile.FLAC, audiofiletest.FLAC(44100, 44100)},
		{"voice.ogg", audiofile.OGG, audiofiletest.OGGVorbis(44100, 44100)},
		{"voice.m4a", audiofile.M4A, audiofiletest.M4A(0, 44100, 44100, true)},
	}
}

// An audio file attached to the first message or a later one is an asset of the Project with the type audio and the type of its bytes, is kept as it came, and is
// neither an image of the Brief's Assets nor a clip of its References: it only rides on the message.
func TestAttachments_AnAudioFileIsAnAssetOfTypeAudioAndStaysOutOfTheBrief(t *testing.T) {
	for _, c := range audioFiles() {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			p := f.createProject(t, "音声つきの動画", upload(c.name, c.data))
			brief := f.brief(t, p.ID)
			assert.Empty(t, brief.Assets)
			assert.Empty(t, brief.References)

			atts := f.thread(t, p.ID)[0].Attachments
			require.Len(t, atts, 1)
			assert.Equal(t, domain.AssetAudio, atts[0].Type)
			assert.Equal(t, c.mime, atts[0].MimeType)
			file, err := f.svc.Projects.Asset(context.Background(), p.ID, atts[0].ID)
			require.NoError(t, err)
			stored, err := os.ReadFile(file.Path)
			require.NoError(t, err)
			assert.Equal(t, c.data, stored, "the file is kept as it came: nothing is decoded")

			postMessage(t, f.svc, p.ID, service.PostMessageInput{Content: "もう一つ", Uploads: []service.Upload{upload(c.name, c.data)}})
			brief = f.brief(t, p.ID)
			assert.Empty(t, brief.Assets, "an audio file sent later stays out of the Brief too")
			assert.Empty(t, brief.References)
		})
	}
}

// What is named for audio but is not audio is refused whatever the name says: the bytes must be the kind the extension names. Nothing stays on disk.
func TestAttachments_ARenamedFileIsNotAnAudioFile(t *testing.T) {
	wav, mp3 := audiofiletest.WAV(16000, 16000), audiofiletest.MP3(true, 20)
	huge := service.Upload{Name: "big.wav", Size: audiofile.MaxBytes + 1, Open: func() (io.ReadCloser, error) {
		t.Fatal("an oversized upload must be rejected before it is read")
		return nil, nil
	}}
	for _, c := range []struct {
		name   string
		upload service.Upload
	}{
		{"an image named wav", upload("fake.wav", pngBytes)},
		{"a text named mp3", upload("fake.mp3", []byte("MZ\x90\x00 not a sound"))},
		{"an mp3 named wav", upload("fake.wav", mp3)},
		{"a wav named mp3", upload("fake.mp3", wav)},
		{"a wav named flac", upload("fake.flac", wav)},
		{"a wav named ogg", upload("fake.ogg", wav)},
		{"a wav named m4a", upload("fake.m4a", wav)},
		{"a video named m4a", upload("fake.m4a", mp4Bytes)},
		{"a wav named png", upload("fake.png", wav)},
		{"a wav named mp4", upload("fake.mp4", wav)},
		{"an audio kind that is not taken", upload("fake.aac", wav)},
		{"an empty wav", upload("empty.wav", nil)},
		{"a wav over the limit", huge},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "hi", Uploads: []service.Upload{c.upload}})
			requireInvalid(t, err, "attachments")
			list, err := f.svc.Projects.List(context.Background())
			require.NoError(t, err)
			assert.Empty(t, list, "no project is created")
			entries, _ := os.ReadDir(f.assetsDir)
			assert.Empty(t, entries, "no file is left on disk")
		})
	}
}

// The refusal of an unknown file names the audio kinds too, and an audio file counts as one of the attachments of a message.
func TestAttachments_TheRefusalNamesTheAudioKindsAndAnAudioFileCountsAsAnAttachment(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "hi", Uploads: []service.Upload{upload("run.exe", pngBytes)}})
	var ve *service.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Message, "音声: wav/mp3/flac/ogg/m4a")

	tooMany := make([]service.Upload, service.MaxAttachments+1)
	for i := range tooMany {
		tooMany[i] = upload("a.wav", audiofiletest.WAV(8000, 8000))
	}
	_, err = f.svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "hi", Uploads: tooMany})
	requireInvalid(t, err, "attachments")
}

// The Reference Library holds pictures only: an audio file is refused there, with the message of a video.
func TestReferenceUpload_RefusesAnAudioFile(t *testing.T) {
	f := newFixture(t)
	p := f.createProject(t, "商品CMを作りたい")
	wav := audiofiletest.WAV(16000, 16000)
	_, err := f.svc.Reference.Upload(context.Background(), p.ID, service.UploadReferenceInput{
		File: service.Upload{Name: "voice.wav", Size: int64(len(wav)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(wav)), nil }},
		Kind: string(domain.RefCharacter), Name: "声",
	})
	requireInvalid(t, err, "file")
}

// A Project's audio asset travels in the bundle: exported and imported into another instance, the asset keeps its type and its type of file, and the file is there.
func TestBundle_RoundTripKeepsAnAudioAsset(t *testing.T) {
	ctx := context.Background()
	first := newFixture(t)
	data := audiofiletest.M4A(0, 44100, 44100, true)
	p := first.createProject(t, "音声つきの動画", upload("voice.m4a", data))
	asset := first.thread(t, p.ID)[0].Attachments[0]

	exp, err := first.svc.Bundle.Prepare(ctx, p.ID)
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, exp.Write(&out))

	second := newFixture(t)
	imported, err := second.svc.Bundle.Import(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	require.NoError(t, err)
	got, err := second.svc.Projects.Asset(ctx, imported.ID, asset.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.AssetAudio, got.Asset.Type)
	assert.Equal(t, audiofile.M4A, got.Asset.MimeType)
	stored, err := os.ReadFile(got.Path)
	require.NoError(t, err)
	assert.Equal(t, data, stored)
}
