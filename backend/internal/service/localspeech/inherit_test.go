package localspeech_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/gptsovits"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service/localspeech"
	"github.com/ousiassllc/mam/backend/internal/service/voicerefsvc"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

type fiveSeconds struct{}

func (fiveSeconds) AudioDuration(context.Context, string) (float64, error) { return 5, nil }

// recording is a PCM wav of n samples at 8 kHz (what the user uploads as the reference voice).
func recording(n int) []byte {
	var b bytes.Buffer
	data := bytes.Repeat([]byte{1, 2}, n)
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)})
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

func upload(name string, data []byte) voicerefsvc.File {
	return voicerefsvc.File{Name: name, Size: int64(len(data)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
}

// apiV2 is a fake GPT-SoVITS api_v2.py: it reads the reference recording from the path it is given, as the real one does, and answers
// with a wav tone.
type apiV2 struct {
	mu    sync.Mutex
	calls []apiCall
	tone  []byte
}

type apiCall struct {
	Text       string `json:"text"`
	TextLang   string `json:"text_lang"`
	RefPath    string `json:"ref_audio_path"`
	PromptText string `json:"prompt_text"`
	PromptLang string `json:"prompt_lang"`
	RefSHA     [32]byte
}

func (a *apiV2) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var c apiCall
	raw, _ := io.ReadAll(r.Body)
	if r.URL.Path != "/tts" || json.Unmarshal(raw, &c) != nil {
		http.Error(w, `{"message":"bad request"}`, http.StatusBadRequest)
		return
	}
	ref, err := os.ReadFile(c.RefPath)
	if err != nil {
		http.Error(w, `{"message":"tts failed","Exception":"`+c.RefPath+` not exists"}`, http.StatusBadRequest)
		return
	}
	c.RefSHA = sha256.Sum256(ref)
	a.mu.Lock()
	a.calls = append(a.calls, c)
	a.mu.Unlock()
	_, _ = w.Write(a.tone)
}

func toneWav(t *testing.T) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg is not installed")
	}
	out := filepath.Join(t.TempDir(), "tone.wav")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=300:duration=1", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make tone: %v: %s", err, b)
	}
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	return data
}

type fixedServer struct{ url string }

func (f fixedServer) Ensure(context.Context) (string, string, error) { return f.url, "test-token", nil }

func exec1(t *testing.T, db *repository.DB, q string, args ...any) {
	t.Helper()
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// Two Episodes of a Series speak with the reference voice of the canon Character: the lookup of the Episodes' Characters (Issue #919)
// gives the same recording, and the local voice calls GPT-SoVITS with that same file.
func TestTwoEpisodesOfASeriesAreSpokenWithTheSameReferenceVoice(t *testing.T) {
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	ctx := context.Background()
	db := testdb.Open(t)
	series, world := repository.NewSeriesRepository(db), repository.NewWorldRepository(db)
	refs := voicerefsvc.New(voicerefsvc.Deps{DB: db, Series: series, Projects: repository.NewProjectRepository(db), World: world, AssetsDir: t.TempDir(), Probe: fiveSeconds{}})
	exec1(t, db, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`)
	exec1(t, db, `INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'ユイ'), ('c2', 'p1', ?)`, voiceref.NarrationName)
	s, err := series.Create(ctx, "ユイの日常", "p1")
	require.NoError(t, err)
	canon, err := series.Canon(ctx, s.ID)
	require.NoError(t, err)
	canonIDs := map[string]string{}
	for _, c := range canon.Characters {
		canonIDs[c.Name] = c.ID
	}
	yuiWav, narrationWav := recording(100), recording(120)
	_, err = refs.SetForSeries(ctx, s.ID, canonIDs["ユイ"], voicerefsvc.Input{File: upload("yui.wav", yuiWav), Text: ptr("正本の声")})
	require.NoError(t, err)
	_, err = refs.SetForSeries(ctx, s.ID, canonIDs[voiceref.NarrationName], voicerefsvc.Input{File: upload("na.wav", narrationWav)})
	require.NoError(t, err)
	for _, id := range []string{"ep2", "ep3"} {
		_, err = series.CreateEpisode(ctx, s.ID, "", "", domain.Project{
			ID: id, State: domain.StateDraft, Mode: domain.ModeAuto, ConfirmationLevel: domain.ConfirmKeyCheck,
			ProductionTemplate: domain.TemplateProductCM, QualityPolicy: domain.QualityBalanced,
		})
		require.NoError(t, err)
	}

	api := &apiV2{tone: toneWav(t)}
	srv := httptest.NewServer(api)
	defer srv.Close()
	director := audiodirector.New(audiodirector.Deps{
		Providers: audiodirector.Providers{LocalSpeech: gptsovits.New(fixedServer{srv.URL}, gptsovits.Config{}, nil)},
		Tools:     tools, Root: t.TempDir(),
	})
	scenes := []audiodirector.Scene{{Index: 1, Start: 0, End: 6, Clips: 2, Lines: []string{"ユイ: こんにちは", "ナレーション: 朝になりました"}}}
	for _, ep := range []string{"ep2", "ep3"} {
		voices, err := localspeech.Voices(ctx, refs, ep, "", scenes)
		require.NoError(t, err, ep)

		tl, err := director.Produce(ctx, audiodirector.Input{
			ProjectID: ep, EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: scenes, Seconds: 6,
			VoiceProvider: localvoice.Local, LocalVoices: voices,
		})

		require.NoError(t, err, ep)
		assert.Equal(t, 1, tl.Count(domain.LayerDialogue), ep)
		assert.Equal(t, 1, tl.Count(domain.LayerNarration), ep)
	}

	require.Len(t, api.calls, 4, "two lines of each Episode")
	byLine := map[string][]apiCall{}
	for _, c := range api.calls {
		byLine[c.Text] = append(byLine[c.Text], c)
	}
	yui, na := byLine["こんにちは"], byLine["朝になりました"]
	require.Len(t, yui, 2)
	require.Len(t, na, 2)
	assert.Equal(t, yui[0].RefPath, yui[1].RefPath, "the Episodes share the canon's recording")
	assert.Equal(t, sha256.Sum256(yuiWav), yui[0].RefSHA, "the content of the file GPT-SoVITS read is the canon's recording")
	assert.Equal(t, yui[0].RefSHA, yui[1].RefSHA)
	assert.Equal(t, "正本の声", yui[0].PromptText)
	assert.Equal(t, "ja", yui[0].PromptLang)
	assert.Equal(t, "ja", yui[0].TextLang)
	assert.Equal(t, sha256.Sum256(narrationWav), na[0].RefSHA, "the narration is the voice of the Character named ナレーション")
	assert.Equal(t, na[0].RefSHA, na[1].RefSHA)
	assert.NotEqual(t, yui[0].RefSHA, na[0].RefSHA)
	assert.Empty(t, na[0].PromptText, "a recording without a transcript is sent without a prompt text")
}

func ptr(s string) *string { return &s }

// An Episode whose Character has no reference voice is refused with its name, before the server is asked for anything.
func TestAnEpisodeWhoseCharacterHasNoReferenceVoiceIsRefusedByName(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)
	refs := voicerefsvc.New(voicerefsvc.Deps{DB: db, Series: repository.NewSeriesRepository(db), Projects: repository.NewProjectRepository(db), World: repository.NewWorldRepository(db), AssetsDir: t.TempDir(), Probe: fiveSeconds{}})
	exec1(t, db, `INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'key_check', 'product_cm', 'balanced')`)
	exec1(t, db, `INSERT INTO character (id, project_id, name) VALUES ('c1', 'p1', 'ユイ')`)
	scenes := []audiodirector.Scene{{Lines: []string{"ユイ: こんにちは", "ケン: やあ", "ナレーション: 朝"}}}

	voices, err := localspeech.Voices(ctx, refs, "p1", "", scenes)
	require.NoError(t, err)
	api := &apiV2{}
	srv := httptest.NewServer(api)
	defer srv.Close()
	local := gptsovits.New(fixedServer{srv.URL}, gptsovits.Config{}, nil)
	d := audiodirector.New(audiodirector.Deps{Providers: audiodirector.Providers{LocalSpeech: local}, Root: t.TempDir()})

	_, err = d.Produce(ctx, audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityFast, Scenes: scenes, Seconds: 6, VoiceProvider: localvoice.Local, LocalVoices: voices,
	})

	var mv *localvoice.MissingVoiceError
	require.ErrorAs(t, err, &mv)
	assert.Equal(t, []string{"ユイ", "ケン", voiceref.NarrationName}, mv.Names())
	assert.ErrorIs(t, err, voicerefsvc.ErrNoReference, "the Character exists but has no recording")
	assert.ErrorIs(t, err, voicerefsvc.ErrNoCharacter, "nobody is named ケン")
	text, ok := localspeech.Reason(err)
	require.True(t, ok)
	assert.Contains(t, text, fmt.Sprintf("「%s」", "ケン"))
	assert.Empty(t, api.calls)
}
