package runtimes_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	if lrtest.ServeIfAsked() {
		return
	}
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// screen is the model manager screen with the local runtimes over fakes.
type screen struct{ *screentest.Screen }

func newRuntimeScreen(t *testing.T, opts ...lrtest.Option) *screen {
	t.Helper()
	return &screen{screentest.NewWithRuntimes(t, opts...)}
}

func runtimePart(id string, rest ...string) string {
	return paths.ModelManagerPart(append([]string{"runtimes", id}, rest...)...)
}

func (s *screen) installRuntime() {
	s.T.Helper()
	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "install"), nil)
	require.Equal(s.T, http.StatusAccepted, rec.Code, rec.Body.String())
	s.waitRuntimeJob(screentest.JobID(s.T, rec.Body.String()))
}

// waitRuntimeJob runs the queue until the job of a runtime ended.
func (s *screen) waitRuntimeJob(id string) {
	s.T.Helper()
	s.Rig.Start()
	s.Rts.Wait(id)
}

func TestRuntimes_ThePageShowsTheCardWithItsWeightsSourceLicenseAndTheUnclearNote(t *testing.T) {
	s := newRuntimeScreen(t)
	body := s.Page()
	assert.Contains(t, body, "ローカル音声・口パク")
	assert.Contains(t, body, `data-testid="mm-runtime"`)
	assert.Contains(t, body, `data-runtime="gpt-sovits"`)
	assert.Contains(t, body, `data-testid="mm-runtime-missing"`)
	assert.Contains(t, body, `hx-post="`+runtimePart(lrtest.ID, "install")+`"`)
	assert.Contains(t, body, "必要な空き容量")
	assert.Contains(t, body, "VRAM の目安")
	assert.Contains(t, body, "MIT License", "the license of the program")

	// The weights are listed before the install (the user sees what comes from where), each with its source link and license.
	assert.Contains(t, body, `data-testid="mm-runtime-assets"`)
	assert.Equal(t, 2, strings.Count(body, `data-testid="mm-runtime-asset"`))
	assert.Contains(t, body, `href="https://huggingface.co/org/repo"`)
	assert.Contains(t, body, `href="https://example.com/mit"`)
	assert.Contains(t, body, "商用利用: 可")
	assert.Contains(t, body, "原本は x。2026-11-01 に SHA-256 の一致を確認した", "the note of a copy says where the original is")

	// The asset whose commercial use is unclear is said before the list, and again in its own row, prominently.
	assert.Contains(t, body, `data-testid="mm-runtime-unclear"`)
	assert.Contains(t, body, "商用利用がはっきりしない重み・依存ファイルが 1 件あります")
	assert.Contains(t, body, "商用利用: はっきりしません")
	assert.Contains(t, body, `data-testid="mm-runtime-asset-notice"`)
	assert.Contains(t, body, "商用で使う前に配布元に確認してください")
	assert.Contains(t, body, `data-commercial="unclear"`)
	assert.Contains(t, body, `data-commercial="yes"`)
	assert.Contains(t, body, `data-testid="mm-runtime-weights" open`, "the list is open when something is unclear")
	assert.Equal(t, 1, strings.Count(body, `data-testid="mm-runtime-asset-notice"`), "a license that is plainly yes carries no warning")
}

func TestRuntimes_AMachineThatCannotRunItSeesWhyAndTheCloudProviderToUse(t *testing.T) {
	for name, tc := range map[string]struct {
		opts   []lrtest.Option
		reason string
	}{
		"macOS":                   {[]lrtest.Option{lrtest.Platform("darwin", "arm64")}, "macOS では GPT-SoVITS の自動導入に対応していません"},
		"an AMD GPU":              {[]lrtest.Option{lrtest.GPUs(gpu.Adapter{Name: "AMD Radeon RX 7800 XT", VendorID: 0x1002, VRAMBytes: 16 << 30})}, "NVIDIA の GPU が見つからないため"},
		"too little video memory": {[]lrtest.Option{lrtest.GPUs(lrtest.NVIDIA("GeForce GTX 1050", 2, 6.1))}, "ビデオメモリは 2.0 GB"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newRuntimeScreen(t, tc.opts...)
			body := s.Page()
			assert.Contains(t, body, `data-testid="mm-runtime-unsupported"`)
			assert.Contains(t, body, tc.reason)
			assert.Contains(t, body, `data-testid="mm-runtime-fallback"`)
			assert.Contains(t, body, "ElevenLabs", "the cloud provider to use instead")
			assert.Contains(t, body, "この PC では使えません")
			assert.NotContains(t, body, `data-testid="mm-runtime-install"`, "no install button where it cannot be installed")
			assert.Contains(t, body, `data-testid="mm-runtime-assets"`, "the sources and licenses are shown on such a machine too")

			rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "install"), nil)
			assert.Equal(t, http.StatusConflict, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.reason, "the refusal says the same sentence")
			assert.Contains(t, rec.Body.String(), `role="alert"`)
			rec = s.Do(http.MethodPost, runtimePart(lrtest.ID, "start"), nil)
			assert.Equal(t, http.StatusConflict, rec.Code)
			assert.NoDirExists(t, s.Rts.Layout.Root, "nothing is made on a machine that cannot run it")
		})
	}
}

func TestRuntimes_InstallShowsProgressThenTheInstalledCardWithTheWeightsChecked(t *testing.T) {
	s := newRuntimeScreen(t)
	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="mm-progress"`)
	assert.Contains(t, body, `hx-trigger="every 2s"`, "the card polls itself while the install runs")
	assert.Contains(t, body, "中断する")
	job := screentest.JobID(t, body)
	assert.Contains(t, body, `hx-get="`+runtimePart(lrtest.ID)+`?job=`+job+`"`)
	assert.Contains(t, body, "導入を始めました")

	s.waitRuntimeJob(job)
	done := s.Get(runtimePart(lrtest.ID) + "?job=" + job).Body.String()
	assert.Contains(t, done, "GPT-SoVITS を導入しました")
	assert.Contains(t, done, `data-testid="mm-runtime-state"`)
	assert.Contains(t, done, `data-state="stopped"`)
	assert.NotContains(t, done, `hx-trigger="every 2s"`, "polling stops once the install ended")
	assert.NotContains(t, done, "重みが不足")
	assert.NotContains(t, done, `data-testid="mm-runtime-weights-install"`, "nothing to install: every file is verified")
	assert.Equal(t, 2, strings.Count(done, `data-state="ok"`), "both weights are there and checked")
	assert.FileExists(t, s.Rts.WeightPath(lrtest.WeightDest))

	// A second install request while one runs would join it; once it ended the card offers the actions.
	for _, want := range []string{"起動", "削除", `data-testid="mm-runtime-autostart"`} {
		assert.Contains(t, done, want)
	}
}

func TestRuntimes_AMissingWeightShowsInstalledWeightsMissingAndStartIsRefusedBeforeItRuns(t *testing.T) {
	s := newRuntimeScreen(t)
	s.installRuntime()
	require.NoError(t, os.Remove(s.Rts.WeightPath(lrtest.WeightDest)))

	card := s.Get(runtimePart(lrtest.ID)).Body.String()
	assert.Contains(t, card, "導入済み（重みが不足）")
	assert.Contains(t, card, `data-testid="mm-runtime-weights-message"`)
	assert.Contains(t, card, `data-testid="mm-runtime-weights-install"`)
	assert.Contains(t, card, `hx-post="`+runtimePart(lrtest.ID, "weights")+`"`)

	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "start"), nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "refused at once, not left to fail at the start")
	assert.Contains(t, rec.Body.String(), "重みが不足しています（model）")
	assert.Equal(t, "stopped", string(s.Rts.Svc.List(t.Context())[0].Process.State))

	rec = s.Do(http.MethodPost, runtimePart(lrtest.ID, "weights"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "重みの確認と取得を始めました")
	s.waitRuntimeJob(screentest.JobID(t, rec.Body.String()))
	fixed := s.Get(runtimePart(lrtest.ID)).Body.String()
	assert.NotContains(t, fixed, "導入済み（重みが不足）")
	assert.FileExists(t, s.Rts.WeightPath(lrtest.WeightDest))
}

func TestRuntimes_AWeightWhoseSourceIsGoneIsToldWithWhereTheOriginalIs(t *testing.T) {
	s := newRuntimeScreen(t)
	s.installRuntime()
	require.NoError(t, os.Remove(s.Rts.WeightPath(lrtest.ExtraDest)))
	s.Rts.Host.Gone[lrtest.ExtraURLPath] = true

	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "weights"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	job := screentest.JobID(t, rec.Body.String())
	s.waitRuntimeJob(job)
	failed := s.Get(runtimePart(lrtest.ID) + "?job=" + job).Body.String()
	assert.Contains(t, failed, `role="alert"`)
	assert.Contains(t, failed, "取得元が消えています（extra、HTTP 404）")
	assert.Contains(t, failed, "原本は x。2026-11-01 に SHA-256 の一致を確認した")
}

func TestRuntimes_StartStopAutoStartAndRemove(t *testing.T) {
	s := newRuntimeScreen(t)
	s.installRuntime()

	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "start"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`, "the card polls until the server answers")
	require.Eventually(t, func() bool {
		return strings.Contains(s.Get(runtimePart(lrtest.ID)).Body.String(), `data-state="running"`)
	}, 20*time.Second, 50*time.Millisecond)
	running := s.Get(runtimePart(lrtest.ID)).Body.String()
	assert.Contains(t, running, "127.0.0.1:")
	assert.Contains(t, running, "停止")

	rec = s.Do(http.MethodPost, runtimePart(lrtest.ID, "stop"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-state="stopped"`)
	assert.Contains(t, rec.Body.String(), "GPT-SoVITS を停止しました")

	rec = s.Do(http.MethodPut, runtimePart(lrtest.ID, "autostart"), url.Values{"on": {"1"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "オン")
	assert.True(t, s.Rts.Svc.List(t.Context())[0].AutoStart)
	rec = s.Do(http.MethodPut, runtimePart(lrtest.ID, "autostart"), url.Values{"on": {"0"}})
	assert.Contains(t, rec.Body.String(), "オフ")
	assert.False(t, s.Rts.Svc.List(t.Context())[0].AutoStart)

	rec = s.Do(http.MethodDelete, runtimePart(lrtest.ID), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "GPT-SoVITS を削除しました")
	assert.Contains(t, rec.Body.String(), `data-testid="mm-runtime-missing"`)
	assert.NoFileExists(t, s.Rts.WeightPath(lrtest.WeightDest), "the weights go with the environment")
}

func TestRuntimes_RefusalsOfTheStateAndAnUnknownRuntime(t *testing.T) {
	s := newRuntimeScreen(t)
	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "start"), nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "導入されていません")
	rec = s.Do(http.MethodPost, runtimePart(lrtest.ID, "weights"), nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "the weights of a runtime that is not installed")

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, runtimePart("nope")}, {http.MethodPost, runtimePart("nope", "install")}, {http.MethodPost, runtimePart("nope", "start")},
		{http.MethodPost, runtimePart("nope", "stop")}, {http.MethodDelete, runtimePart("nope")}, {http.MethodPost, runtimePart("nope", "weights")},
		{http.MethodPut, runtimePart("nope", "autostart")},
	} {
		rec := s.Do(tc.method, tc.path, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, tc.method+" "+tc.path)
		assert.Contains(t, rec.Body.String(), "対象が見つかりません")
	}

	// An install that is running is not removed or started.
	rec = s.Do(http.MethodPost, runtimePart(lrtest.ID, "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := screentest.JobID(t, rec.Body.String())
	assert.Equal(t, http.StatusConflict, s.Do(http.MethodDelete, runtimePart(lrtest.ID), nil).Code)
	assert.Equal(t, http.StatusConflict, s.Do(http.MethodPost, runtimePart(lrtest.ID, "start"), nil).Code)
	s.waitRuntimeJob(job)
}

func TestRuntimes_TheCancelButtonOfTheCardStopsItsInstall(t *testing.T) {
	s := newRuntimeScreen(t)
	release := s.Rts.Host.Hold(lrtest.WeightURLPath)
	defer release()
	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := screentest.JobID(t, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `hx-post="`+paths.ModelManagerPart("jobs", job, "cancel")+`"`)
	assert.Contains(t, rec.Body.String(), `hx-target="#mm-runtime-gpt-sovits"`, "the answer is the card, not the model list")
	s.Rig.Start()
	s.Rts.Host.Reached(t) // the package is in; the first weight is being fetched

	rec = s.Do(http.MethodPost, paths.ModelManagerPart("jobs", job, "cancel"), nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-runtime="gpt-sovits"`)
	assert.Contains(t, rec.Body.String(), "導入を中断しています")
	st := s.Rts.Wait(job)
	assert.Contains(t, st.Error, "導入を中断しました")
	ended := s.Get(runtimePart(lrtest.ID) + "?job=" + job).Body.String()
	assert.Contains(t, ended, "導入を中断しました")
	assert.Contains(t, ended, "導入済み（重みが不足）", "the package is installed; the weights are what is left")
}

func TestRuntimes_CancellingAJobThatIsNotRunningIsNotFound(t *testing.T) {
	s := newRuntimeScreen(t)
	rec := s.Do(http.MethodPost, runtimePart(lrtest.ID, "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := screentest.JobID(t, rec.Body.String())
	rec = s.Do(http.MethodPost, paths.ModelManagerPart("jobs", job, "cancel"), nil) // queued: the queue has not started
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-runtime="gpt-sovits"`)
	s.waitRuntimeJob(job)
}

func TestRuntimes_AScreenWithoutTheServiceHasNoSectionAndNoRoutes(t *testing.T) {
	s := &screen{screentest.New(t)}
	assert.NotContains(t, s.Page(), "ローカル音声・口パク")
	assert.Equal(t, http.StatusNotFound, s.Do(http.MethodPost, runtimePart(lrtest.ID, "install"), nil).Code)
}

func TestRuntimes_TheCardUsesTheLabelsOfScreenReadersAndNeverAKeyOrAToken(t *testing.T) {
	s := newRuntimeScreen(t)
	s.installRuntime()
	body := s.Get(runtimePart(lrtest.ID)).Body.String()
	assert.Contains(t, body, `aria-labelledby="mm-runtime-gpt-sovits-title"`)
	assert.Contains(t, body, `id="mm-runtime-gpt-sovits-title"`)
	assert.NotContains(t, strings.ToLower(body), "token")
}
