package providerbackends_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/viaprovider"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestOpenRouterKling3TextToVideoSubmitsPollsAndDownloadsWithTheKey(t *testing.T) {
	var mu sync.Mutex
	var seenReqs []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := seen{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization")}
		if r.URL.RawQuery != "" {
			s.path += "?" + r.URL.RawQuery
		}
		if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
			_ = json.Unmarshal(raw, &s.body)
		}
		mu.Lock()
		seenReqs = append(seenReqs, s)
		n := len(seenReqs)
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/videos":
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"job1","polling_url":"x","status":"pending"}`))
		case r.URL.Path == "/videos/job1":
			status := "in_progress"
			if n >= 4 {
				status = "completed"
			}
			_, _ = w.Write([]byte(`{"id":"job1","status":"` + status + `"}`))
		case r.URL.Path == "/videos/job1/content":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.OpenRouter, map[string]string{"openrouter": "or-dummy-key"})

	res, err := generate(t, b, domain.ModelKling3, modeladapter.VideoRequest{
		Prompt: "a fox runs", NegativePrompt: "blur", DurationSeconds: 7, Resolution: "720p", AspectRatio: "16:9",
	})

	require.NoError(t, err)
	readVideo(t, res)
	mu.Lock()
	defer mu.Unlock()
	submit := seenReqs[0]
	assert.Equal(t, "Bearer or-dummy-key", submit.auth)
	assert.Equal(t, "kwaivgi/kling-v3.0-std", submit.body["model"], "720p is the standard tier")
	assert.EqualValues(t, 7, submit.body["duration"])
	assert.Equal(t, "16:9", submit.body["aspect_ratio"])
	assert.Equal(t, false, submit.body["generate_audio"], "the price estimated is the silent one")
	assert.Equal(t, "a fox runs Avoid: blur.", submit.body["prompt"], "no negative field: folded into the prompt")
	assert.NotContains(t, submit.body, "frame_images")
	assert.NotContains(t, submit.body, "resolution", "the slug chooses the tier")
	last := seenReqs[len(seenReqs)-1]
	assert.Equal(t, "/videos/job1/content?index=0", last.path)
	assert.Equal(t, "Bearer or-dummy-key", last.auth, "the content URL is not presigned")
}

func TestOpenRouterKling3ImageToVideoSendsFramesAndTheProTier(t *testing.T) {
	var submitBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/videos":
			_ = json.NewDecoder(r.Body).Decode(&submitBody)
			_, _ = w.Write([]byte(`{"id":"j","status":"pending"}`))
		case r.URL.Path == "/videos/j":
			_, _ = w.Write([]byte(`{"id":"j","status":"completed"}`))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		}
	}))
	defer srv.Close()
	_, b := backends(t, srv, viaprovider.OpenRouter, map[string]string{"openrouter": "k"})

	res, err := generate(t, b, domain.ModelKling3, modeladapter.VideoRequest{
		Prompt: "p", DurationSeconds: 5, Resolution: "1080p", AspectRatio: "16:9", FirstFrame: frame("A"), LastFrame: frame("B"),
	})

	require.NoError(t, err)
	readVideo(t, res)
	assert.Equal(t, "kwaivgi/kling-v3.0-pro", submitBody["model"], "1080p is the pro tier")
	assert.NotContains(t, submitBody, "aspect_ratio", "the frame sets the ratio")
	frames, _ := submitBody["frame_images"].([]any)
	require.Len(t, frames, 2)
	first, _ := frames[0].(map[string]any)
	last, _ := frames[1].(map[string]any)
	assert.Equal(t, "first_frame", first["frame_type"])
	assert.Equal(t, "last_frame", last["frame_type"])
	assert.Equal(t, map[string]any{"url": dataURI("A")}, first["image_url"])
	assert.Equal(t, map[string]any{"url": dataURI("B")}, last["image_url"])
}

func TestOpenRouterFailedJobIsRefusedWithItsReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"j","status":"pending"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"j","status":"failed","error":"Content policy violation"}`))
	}))
	defer srv.Close()
	const secret = "or-secret-7f3a91c2d4e8"
	_, b := backends(t, srv, viaprovider.OpenRouter, map[string]string{"openrouter": secret})

	_, err := generate(t, b, domain.ModelKling3, modeladapter.VideoRequest{Prompt: "p", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9"})

	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrRejected)
	assert.ErrorIs(t, err, modeladapter.ErrAfterSubmit, "the job exists at the provider: it must not be submitted again")
	assert.Contains(t, err.Error(), "Content policy violation")
	assert.NotContains(t, err.Error(), secret, "the key does not leak into the error")
}

// The prompt Prepare makes for Kling 3.0 is up to 2400 characters and its avoid list up to 400: OpenRouter has no field for the avoid list, so the backend
// folds it in. What reaches OpenRouter must still be within the 2400 characters of the model, with the Constraints and the avoid list whole.
func TestOpenRouterKling3PromptOfAShotStaysWithinTheLimitWithTheAvoidListFolded(t *testing.T) {
	var submitBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/videos":
			_ = json.NewDecoder(r.Body).Decode(&submitBody)
			_, _ = w.Write([]byte(`{"id":"j","status":"pending"}`))
		case r.URL.Path == "/videos/j":
			_, _ = w.Write([]byte(`{"id":"j","status":"completed"}`))
		default:
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte(videoBytes))
		}
	}))
	defer srv.Close()
	cfg, b := backends(t, srv, viaprovider.OpenRouter, map[string]string{"openrouter": "k"})
	job := modeladapter.VideoJob{Spec: domain.ShotSpec{ShotObjective: strings.Repeat("A very long objective. ", 150), Constraints: []string{"do_not_add_characters", "preserve_logo"}}, DurationSeconds: 5}
	for i := range 60 {
		job.Spec.Acting = append(job.Spec.Acting, domain.ActingBeat{Time: "0.0-0.5", Action: "slowly_turns_the_head_towards_the_window_" + strings.Repeat("x", i%7)})
	}
	req, err := cfg.Prepare(modeladapter.VideoTarget{Model: domain.ModelKling3, Resolution: "720p"}, job)
	require.NoError(t, err)

	res, err := generate(t, b, domain.ModelKling3, req)

	require.NoError(t, err)
	res.File.Discard()
	prompt, _ := submitBody["prompt"].(string)
	assert.LessOrEqual(t, utf8.RuneCountInString(prompt), 2400)
	assert.Contains(t, prompt, "Avoid: ", "the avoid list is folded in")
	assert.Contains(t, prompt, "distorted face", "and kept whole, with the Constraints")
	assert.Contains(t, prompt, "keep the logo and label text sharp and undistorted")
}
