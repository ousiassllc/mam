package llamaservertest_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/llm/localllm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/proc"
	"github.com/ousiassllc/mam/backend/internal/service/llamaserver"
	"github.com/ousiassllc/mam/backend/internal/service/llmlocal"
	"github.com/ousiassllc/mam/backend/internal/service/llmmodels"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices"
)

// wiring is what the app builds: the settings service that knows the managed server, and the provider "local" that calls it.
type wiring struct {
	r        *rig
	settings *llmlocal.Service
	backend  llm.Backend
	server   llamaserver.Server
	choices  runtimechoices.Service
}

func wire(t *testing.T, r *rig, makeRoom func(context.Context) (bool, error)) *wiring {
	t.Helper()
	keys := credentialtest.New()
	require.NoError(t, keys.Set("local", "key-for-another-server"))
	settings := llmlocal.New(filepath.Join(t.TempDir(), "llm.json"), false, keys, nil)
	server := llamaserver.Server{Runtime: r.svc, MakeRoom: makeRoom, MaxWait: 300 * time.Millisecond, PollEvery: 10 * time.Millisecond}
	settings.SetManaged(server)
	backends := map[llm.Provider]llm.Backend{}
	localllm.Add(backends, keys, nil, settings.BaseURL, localllm.WithManaged(settings))
	return &wiring{r: r, settings: settings, backend: backends[llm.ProviderLocal], server: server, choices: runtimechoices.Service{Runtimes: r.svc}}
}

func (w *wiring) useManaged(on bool) {
	w.r.t.Helper()
	require.NoError(w.r.t, w.settings.Save(llmlocal.SaveInput{UseManaged: &on}))
}

func (w *wiring) state() proc.State {
	st, err := w.r.svc.Status(ctx(), "llama-cpp")
	require.NoError(w.r.t, err)
	return st.Process.State
}

// requests is what the fake llama-server was asked to complete, read with the key of its start.
func (w *wiring) requests() []map[string]any {
	w.r.t.Helper()
	url, key, err := w.server.Endpoint(ctx())
	require.NoError(w.r.t, err)
	req, _ := http.NewRequest(http.MethodGet, url+"/_requests", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	require.NoError(w.r.t, err)
	defer func() { _ = res.Body.Close() }()
	var out []map[string]any
	require.NoError(w.r.t, json.NewDecoder(res.Body).Decode(&out))
	return out
}

func ready(t *testing.T) (*rig, *wiring) {
	t.Helper()
	r := newRig(t)
	r.install()
	w := wire(t, r, nil)
	w.useManaged(true)
	return r, w
}

// While the user has chosen "この PC の llama.cpp を使う", the first call starts the server (the lazy start) and goes to the port mam chose, with the key
// of the start. The address of the settings and the key stored for another server are not used.
func TestTheProviderStartsTheManagedServerOnTheFirstCallAndUsesItsPortAndKey(t *testing.T) {
	r, w := ready(t)
	assert.Equal(t, proc.StateStopped, w.state(), "nothing runs until a call needs it")
	assert.True(t, w.settings.InUse())

	done, err := w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u", JSON: true})

	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, done.Text)
	assert.Equal(t, proc.StateRunning, w.state())
	st, _ := r.svc.Status(ctx(), "llama-cpp")
	assert.Equal(t, "http://127.0.0.1:"+portOf(st.Process.URL), st.Process.URL)
	assert.Equal(t, &llm.Usage{InputTokens: 7, OutputTokens: 9}, done.Usage)
	info, ok := w.settings.ManagedInfo(ctx())
	require.True(t, ok)
	assert.Equal(t, llmlocal.ManagedRunning, info.State)

	w.useManaged(false)
	assert.False(t, w.settings.InUse(), "back to the address of the settings (nothing listens there: the call fails, it does not reach the managed server)")
	_, err = w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})
	assert.True(t, localllm.IsUnreachable(err))
}

func portOf(url string) string { return url[len("http://127.0.0.1:"):] }

// A role that reads the answer as JSON turns the thinking off (enable_thinking=false, which the chat template of Gemma and Qwen takes) and asks for
// the JSON mode; a role that writes text leaves the thinking of the model alone.
func TestTheJSONRolesTurnThinkingOffOnTheManagedServerAndTheOthersDoNot(t *testing.T) {
	_, w := ready(t)

	json1, err := w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "plan", JSON: true})
	require.NoError(t, err)
	text, err := w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "write"})
	require.NoError(t, err)

	assert.JSONEq(t, `{"ok":true}`, json1.Text, "thinking was off: the model answered at once")
	assert.Equal(t, "本文です。", text.Text, "thinking was on for the writer, and its <think> block is cut off before the text is used")
	reqs := w.requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, map[string]any{"enable_thinking": false}, reqs[0]["chat_template_kwargs"])
	assert.Equal(t, map[string]any{"type": "json_object"}, reqs[0]["response_format"])
	assert.NotContains(t, reqs[1], "chat_template_kwargs", "a role that writes text keeps the thinking the model has")
	assert.NotContains(t, reqs[1], "response_format")
}

// DeepSeek R1 Distill cannot turn thinking off: whatever mam sends, its answer carries the thinking and only the closing </think> (its template opens the tag
// in the prompt). The provider cuts it off before the JSON is read.
func TestDeepSeekThinkingIsCutOffBeforeTheJSONIsRead(t *testing.T) {
	r, w := ready(t)
	require.NoError(t, w.choices.Select(ctx(), "llama-cpp", "deepseek-r1-14b", nil))
	job := r.installWeights()
	require.Equal(t, "succeeded", string(job.State), job.Error)

	done, err := w.backend.Complete(ctx(), "deepseek-r1-14b", llm.Request{User: "plan", JSON: true})

	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, done.Text, "the thinking (which holds a {draft} object of its own) is gone: the JSON is the answer")
	reqs := w.requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, map[string]any{"enable_thinking": false}, reqs[0]["chat_template_kwargs"], "mam asks anyway; this model ignores it")
}

// Choosing another model stops the server; the next call starts it on the new model's file, under the new model's name, and the model list says which
// model the server has (even while it is stopped: it starts at the first call).
func TestSwitchingTheModelRestartsTheManagedServerAndTheModelListFollows(t *testing.T) {
	r, w := ready(t)
	models := llmmodels.New(llmmodels.Options{LocalBaseURL: w.settings.BaseURL, LocalManaged: w.settings.ManagedModels})
	_, err := w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.NoError(t, err)
	list, err := models.List(ctx(), "local")
	require.NoError(t, err)
	assert.Equal(t, []llmmodels.Model{{ID: "gemma-4-12b-it"}}, list.Models, "the model of the managed server, not what an address answers")

	require.NoError(t, w.choices.Select(ctx(), "llama-cpp", "deepseek-r1-14b", nil))
	require.Equal(t, proc.StateStopped, w.state())
	_, err = w.backend.Complete(ctx(), "deepseek-r1-14b", llm.Request{User: "u"})
	require.Error(t, err, "the weight of the new model is not there: the server is not started on it")
	assert.True(t, localllm.IsUnreachable(err))
	assert.ErrorContains(t, err, "重みが足りません")
	list, err = models.List(ctx(), "local")
	require.NoError(t, err)
	assert.Empty(t, list.Models, "a model that cannot run is not offered")

	require.Equal(t, "succeeded", string(r.installWeights().State))
	_, err = w.backend.Complete(ctx(), "deepseek-r1-14b", llm.Request{User: "u"})
	require.NoError(t, err)
	assert.Contains(t, r.argv(), "models/deepseek-r1-14b/DeepSeek-R1-Distill-Qwen-14B.Q4_K_M.gguf")
	assert.Contains(t, r.argv(), "deepseek-r1-14b")
	list, err = models.List(ctx(), "local")
	require.NoError(t, err)
	assert.Equal(t, []llmmodels.Model{{ID: "deepseek-r1-14b"}}, list.Models)

	require.NoError(t, r.svc.Stop("llama-cpp"))
	list, err = models.List(ctx(), "local")
	require.NoError(t, err)
	assert.Equal(t, []llmmodels.Model{{ID: "deepseek-r1-14b"}}, list.Models, "listed while stopped too")
}

// A local image or video generation takes the video memory: the managed server is stopped before it, whatever the Ollama switch says, and the next call of an
// Agent starts it again.
func TestTheManagedServerIsStoppedBeforeAGenerationAndStartsAgainOnTheNextCall(t *testing.T) {
	_, w := ready(t)
	_, err := w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.NoError(t, err)
	require.Equal(t, proc.StateRunning, w.state())

	require.NoError(t, w.settings.BeforeGeneration(ctx()))

	assert.Equal(t, proc.StateStopped, w.state())
	require.NoError(t, w.settings.BeforeGeneration(ctx()), "a stopped server is a no-op")
	_, err = w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.NoError(t, err)
	assert.Equal(t, proc.StateRunning, w.state())
}

// The managed server never starts while the bundled ComfyUI is generating (the two do not share a 16 GB GPU): the call waits for it, and gives up with a
// sentence that says so; when ComfyUI is idle its memory is freed first. A server that runs already is not made to wait.
func TestTheManagedServerWaitsForAGeneratingComfyUIAndFreesAnIdleOne(t *testing.T) {
	r := newRig(t)
	r.install()
	var busyPolls, calls atomic.Int32
	busyFor := int32(3)
	w := wire(t, r, func(context.Context) (bool, error) {
		calls.Add(1)
		return busyPolls.Add(1) <= busyFor, nil
	})
	w.useManaged(true)

	_, err := w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})

	require.NoError(t, err, "it waited for the generation to end, then started")
	assert.GreaterOrEqual(t, calls.Load(), int32(4))
	assert.Equal(t, proc.StateRunning, w.state())
	before := calls.Load()
	_, err = w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.NoError(t, err)
	assert.Equal(t, before, calls.Load(), "a running server holds its memory already: ComfyUI is not asked again")

	require.NoError(t, r.svc.Stop("llama-cpp"))
	busyFor = 1 << 30 // generating for good
	_, err = w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.Error(t, err)
	assert.True(t, localllm.IsUnreachable(err), "the Agent sees a local server that did not answer: its retries and its fallback apply")
	assert.ErrorContains(t, err, "ComfyUI が画像・動画を生成している最中")
	assert.Equal(t, proc.StateStopped, w.state(), "nothing was started on a GPU that is in use")
}

func TestTheManagedServerThatCannotBeCheckedForRoomFailsWithThatError(t *testing.T) {
	r := newRig(t)
	r.install()
	w := wire(t, r, func(context.Context) (bool, error) { return false, errors.New("boom") })
	w.useManaged(true)

	_, err := w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})

	require.Error(t, err)
	assert.ErrorContains(t, err, "boom")
	assert.Equal(t, proc.StateStopped, w.state())
}

// Nothing to call yet: not installed, or this PC cannot run it. The call fails as an unreachable local server with the sentence that says what to do, and the
// card says the same without starting anything.
func TestTheManagedServerThatIsNotInstalledOrNotSupportedSaysSo(t *testing.T) {
	r := newRig(t)
	w := wire(t, r, nil)
	w.useManaged(true)

	_, err := w.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.Error(t, err)
	assert.True(t, localllm.IsUnreachable(err))
	assert.ErrorContains(t, err, "llama.cpp が導入されていません。モデルマネージャーの「ローカル LLM」で llama.cpp を導入してください")
	info, _ := w.settings.ManagedInfo(ctx())
	assert.Equal(t, llmlocal.ManagedNotInstalled, info.State)
	assert.Equal(t, "gemma-4-12b-it", info.Model)
	res, err := w.settings.Test(ctx(), llmlocal.TestInput{})
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "この PC の llama.cpp を使えません")

	mac := newRig(t, platform("darwin", "arm64"))
	wm := wire(t, mac, nil)
	wm.useManaged(true)
	_, err = wm.backend.Complete(ctx(), "gemma-4-12b-it", llm.Request{User: "u"})
	require.Error(t, err)
	assert.ErrorContains(t, err, "macOS（Apple Silicon）では llama.cpp の自動導入に対応していません")
	info, _ = wm.settings.ManagedInfo(ctx())
	assert.Equal(t, llmlocal.ManagedUnsupported, info.State)
}

// The connection test while the managed server is chosen says where it stands and never starts it (loading a model takes much longer than a test).
func TestTheConnectionTestOfTheManagedServerDoesNotStartIt(t *testing.T) {
	_, w := ready(t)

	res, err := w.settings.Test(ctx(), llmlocal.TestInput{BaseURL: "http://127.0.0.1:1"})

	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.Contains(t, res.Message, "この PC の llama.cpp を使います（モデル: Gemma 4 12B-it")
	assert.Equal(t, []string{"gemma-4-12b-it"}, res.Models)
	assert.Equal(t, proc.StateStopped, w.state())
}

// Without a managed server in the app, choosing it is refused and the address of the settings stays in force.
func TestChoosingTheManagedServerWithoutOneInTheAppIsRefused(t *testing.T) {
	settings := llmlocal.New(filepath.Join(t.TempDir(), "llm.json"), false, credentialtest.New(), nil)
	on := true

	err := settings.Save(llmlocal.SaveInput{UseManaged: &on})

	assert.ErrorIs(t, err, llmlocal.ErrManagedUnavailable)
	assert.False(t, settings.InUse())
	assert.False(t, settings.View().UseManaged)
}
