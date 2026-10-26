package comfyui_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyuitest"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localhttp"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/progress"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
)

func backend(t *testing.T, f *comfyuitest.Fake, tpl string, edit func(m *localcfg.Model)) *comfyui.Backend {
	t.Helper()
	tpl2, ok := localcfg.TemplateByID(tpl)
	require.True(t, ok)
	m := tpl2.Model
	m.CostPerImage, m.CostPerSecond = 0.5, 0.01
	if edit != nil {
		edit(&m)
	}
	keys := credentialtest.New()
	c, err := localhttp.New("box", localcfg.Endpoint{Kind: localcfg.KindComfyUI, BaseURL: f.URL()}, keys, t.TempDir())
	require.NoError(t, err)
	c.PollEvery = 10 * time.Millisecond
	return &comfyui.Backend{Client: c, Model: m}
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

type updates struct {
	mu   sync.Mutex
	list []progress.Update
}

func (u *updates) add(x progress.Update) { u.mu.Lock(); u.list = append(u.list, x); u.mu.Unlock() }
func (u *updates) all() []progress.Update {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]progress.Update(nil), u.list...)
}

func str(raw json.RawMessage) string  { var s string; _ = json.Unmarshal(raw, &s); return s }
func num(raw json.RawMessage) float64 { var n float64; _ = json.Unmarshal(raw, &n); return n }

func TestImageWorkflowRunsAndMapsTheValues(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl", nil)
	var got updates
	res, err := b.GenerateImage(progress.With(ctx(t), got.add), "ignored", modeladapter.ImageRequest{Prompt: "a red fox", AspectRatio: "16:9"})
	require.NoError(t, err)

	assert.Equal(t, comfyuitest.PNG, res.Data)
	assert.Equal(t, "image/png", res.MimeType)
	require.NotNil(t, res.ActualCost, "the price the user set is the cost; the Cost Ledger gets a row either way")
	assert.InDelta(t, 0.5, *res.ActualCost, 1e-9)

	g := f.Graph()
	assert.Equal(t, "a red fox", str(g["6"].Inputs["text"]))
	assert.NotEmpty(t, str(g["7"].Inputs["text"]), "the negative prompt")
	assert.Equal(t, 1344.0, num(g["5"].Inputs["width"]))
	assert.Equal(t, 768.0, num(g["5"].Inputs["height"]))
	assert.NotZero(t, num(g["3"].Inputs["seed"]))
	assert.JSONEq(t, `["4",1]`, string(g["6"].Inputs["clip"]), "links the workflow already had are untouched")
	assert.Equal(t, 0, f.Count("POST /upload/image"), "no input image, no upload")

	body := f.Submitted()[0]
	assert.NotEmpty(t, str(body["client_id"]))
	assert.NotEmpty(t, str(body["prompt_id"]), "the client chooses the prompt id, so that the job can be cancelled")
	assert.Equal(t, 1, f.Count("GET /view"))

	var sawKSampler bool
	for _, u := range got.all() {
		assert.LessOrEqual(t, u.Percent, 99)
		if u.Message == "ComfyUI: KSampler" && u.Percent > 0 {
			sawKSampler = true
		}
		assert.NotEqual(t, 90, u.Percent, "another client's progress is ignored")
	}
	assert.True(t, sawKSampler, "progress of the running node reaches the caller: %+v", got.all())
}

func TestReferenceImagesAreUploadedWithTheirExtensionAndInjected(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "img2img-sdxl", nil)
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{
		Prompt: "x", AspectRatio: "1:1", Inputs: []modeladapter.Input{{Data: comfyuitest.PNG, MimeType: "image/png"}, {Data: []byte("jpg"), MimeType: "image/jpeg"}},
	})
	require.NoError(t, err)
	require.Len(t, f.Uploads(), 1, "only the images the workflow takes are uploaded")
	assert.Regexp(t, `^mam-[0-9a-f]{8}-\d+\.png$`, f.Uploads()[0])
	assert.Equal(t, f.Uploads()[0], str(f.Graph()["10"].Inputs["image"]))
}

func TestAWorkflowThatNeedsAnImageRefusesARequestWithout(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "img2img-sdxl", nil)
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x"})
	require.Error(t, err)
	assert.ErrorIs(t, err, modeladapter.ErrRejected, "the request is unusable for this model: the Adapter routes to the alternative")
	assert.Zero(t, f.Count("POST /prompt"))
}

func TestVideoWorkflowMapsFramesSizeAndFirstFrame(t *testing.T) {
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode, f.OutputBody = "clip.mp4", "58", []byte("mp4-bytes")
	b := backend(t, f, "img2video-wan22-5b", nil)
	res, err := b.GenerateVideo(ctx(t), "", modeladapter.VideoRequest{
		Prompt: "pan", NegativePrompt: "blur", DurationSeconds: 5, Resolution: "720p", AspectRatio: "16:9",
		FirstFrame: &modeladapter.Input{Data: comfyuitest.PNG, MimeType: "image/png"},
	})
	require.NoError(t, err)
	assert.Equal(t, "video/mp4", res.MimeType)
	path := filepath(t, res.File)
	assert.Equal(t, []byte("mp4-bytes"), path)
	require.NotNil(t, res.ActualCost)
	assert.InDelta(t, 0.05, *res.ActualCost, 1e-9)

	g := f.Graph()
	assert.Equal(t, 121.0, num(g["55"].Inputs["length"]), "5 s at 24 fps is 4n+1 = 121 frames")
	assert.Equal(t, 24.0, num(g["57"].Inputs["fps"]))
	assert.Equal(t, 1280.0, num(g["55"].Inputs["width"]))
	assert.Equal(t, 736.0, num(g["55"].Inputs["height"]), "rounded to a multiple of 32")
	assert.Equal(t, "blur", str(g["7"].Inputs["text"]))
	assert.Regexp(t, `\.png$`, str(g["56"].Inputs["image"]))
	assert.Equal(t, 1, f.Count("POST /upload/image"))
}

// filepath reads the staged file of a Blob (and discards it).
func filepath(t *testing.T, b modeladapter.Blob) []byte {
	t.Helper()
	data, err := b.Bytes()
	require.NoError(t, err)
	b.Discard()
	return data
}

func TestVideoOutputThatIsNotAVideoIsAnErrorAfterSubmit(t *testing.T) {
	f := comfyuitest.New(t)
	f.OutputFile, f.OutputNode = "frame.png", "58"
	b := backend(t, f, "txt2video-wan22-5b", nil)
	_, err := b.GenerateVideo(ctx(t), "", modeladapter.VideoRequest{Prompt: "x", DurationSeconds: 3, Resolution: "480p", AspectRatio: "16:9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a video file")
	assert.ErrorIs(t, err, modeladapter.ErrAfterSubmit, "the job ran on the GPU already: submitting it again would run it twice")
}

func TestExecutionErrorIsAFailedJobNotAnOutage(t *testing.T) {
	f := comfyuitest.New(t)
	f.FailWith = "CUDA out of memory"
	b := backend(t, f, "txt2img-sdxl", nil)
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CUDA out of memory")
	assert.ErrorIs(t, err, modeladapter.ErrRejected, "422: it counts neither for the breaker nor for a retry")
	assert.False(t, modeladapter.IsUnavailable(err))
}

func TestWorksWithoutAWebSocket(t *testing.T) {
	f := comfyuitest.New(t)
	f.NoSocket = true
	b := backend(t, f, "txt2img-sdxl", nil)
	res, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x"})
	require.NoError(t, err)
	assert.Equal(t, comfyuitest.PNG, res.Data, "only the progress is lost; the history is polled")
}

func TestOutputNodeThatSavedNothingIsAnError(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl", func(m *localcfg.Model) { m.Workflow.OutputNode = "8" }) // VAEDecode: saves no file
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output")
}

func TestCancelUsesTheJobsAPIAndNeverAnUnscopedInterrupt(t *testing.T) {
	f := comfyuitest.New(t)
	f.Hold = make(chan struct{})
	b := backend(t, f, "txt2img-sdxl", nil)
	c, cancel := context.WithCancel(ctx(t))
	done := make(chan error, 1)
	go func() {
		_, err := b.GenerateImage(c, "", modeladapter.ImageRequest{Prompt: "x"})
		done <- err
	}()
	require.Eventually(t, func() bool { return f.Count("GET /history/") > 0 }, 5*time.Second, 5*time.Millisecond)
	cancel()
	require.Error(t, <-done)
	require.Eventually(t, func() bool { return f.Count("POST /api/jobs/") == 1 }, 5*time.Second, 5*time.Millisecond)
	close(f.Hold)

	assert.Equal(t, str(f.Submitted()[0]["prompt_id"]), f.Cancelled()[0], "cancelled by its own id")
	assert.Empty(t, f.Interrupts(), "no unscoped interrupt")
	assert.Empty(t, f.QueueDeletes())
}

func TestCancelFallsBackToAScopedInterruptOrQueueDelete(t *testing.T) {
	for name, running := range map[string]bool{"running": true, "pending": false} {
		t.Run(name, func(t *testing.T) {
			f := comfyuitest.New(t)
			f.CancelStatus = 404 // an older server without /api/jobs
			f.Hold = make(chan struct{})
			b := backend(t, f, "txt2img-sdxl", nil)
			c, cancel := context.WithCancel(ctx(t))
			done := make(chan error, 1)
			go func() {
				_, err := b.GenerateImage(c, "", modeladapter.ImageRequest{Prompt: "x"})
				done <- err
			}()
			require.Eventually(t, func() bool { return f.Count("GET /history/") > 0 }, 5*time.Second, 5*time.Millisecond)
			id := str(f.Submitted()[0]["prompt_id"])
			f.Lock()
			if running {
				f.Running = []string{"other-job", id}
			} else {
				f.Running, f.Pending = []string{"other-job"}, []string{id}
			}
			f.Unlock()
			cancel()
			<-done
			if running {
				require.Eventually(t, func() bool { return f.Count("POST /interrupt") == 1 }, 5*time.Second, 5*time.Millisecond)
			} else {
				require.Eventually(t, func() bool { return f.Count("POST /queue") == 1 }, 5*time.Second, 5*time.Millisecond)
			}
			close(f.Hold)
			if running {
				assert.JSONEq(t, `{"prompt_id":"`+id+`"}`, f.Interrupts()[0])
				assert.Empty(t, f.QueueDeletes())
			} else {
				assert.JSONEq(t, `{"delete":["`+id+`"]}`, f.QueueDeletes()[0])
				assert.Empty(t, f.Interrupts(), "a queued job is deleted, the running job of someone else is not interrupted")
			}
		})
	}
}

func TestAPIKeyIsSentToTheEndpointOnly(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl", nil)
	require.NoError(t, b.Client.Keys.Set("endpoint:box", "sk-secret"))
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x"})
	require.NoError(t, err)
	requests, auths := f.Auths()
	require.NotEmpty(t, requests)
	for i, a := range auths {
		assert.Equal(t, "Bearer sk-secret", a, requests[i])
	}

	h, err := b.Client.Auth("http://evil.example/view")
	require.NoError(t, err)
	assert.Empty(t, h, "a URL outside the base URL never gets the key")
}

func TestImportedGraphIsNotModified(t *testing.T) {
	f := comfyuitest.New(t)
	b := backend(t, f, "txt2img-sdxl", nil)
	before := string(b.Model.Workflow.Graph)
	_, err := b.GenerateImage(ctx(t), "", modeladapter.ImageRequest{Prompt: "x"})
	require.NoError(t, err)
	assert.Equal(t, before, string(b.Model.Workflow.Graph), "each run builds its own copy")
}
