package modelmgr_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/gpu"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func TestPage_ShowsTheEngineCardGPUAndTheCatalogBeforeAnInstall(t *testing.T) {
	s := newScreen(t)
	body := s.page()
	assert.Contains(t, body, `data-testid="mm-engine-missing"`)
	assert.Contains(t, body, `hx-get="`+paths.ModelManagerPart("engine", "gpu")+`"`, "the GPU is detected on load")
	assert.Contains(t, body, `data-testid="mm-catalog-blocked"`)
	assert.Contains(t, body, "先に ComfyUI エンジンを導入してください")
	for _, name := range []string{">A<", ">B<", ">C<"} {
		assert.Contains(t, body, name)
	}
	assert.Contains(t, body, "GPL-3.0")

	gpuBody := s.get(paths.ModelManagerPart("engine", "gpu")).Body.String()
	assert.Contains(t, gpuBody, `data-testid="mm-gpu-recommended"`)
	assert.Contains(t, gpuBody, "NVIDIA")
	assert.Contains(t, gpuBody, `hx-post="`+paths.ModelManagerPart("engine", "install")+`"`)
}

func TestGPU_NoSupportedGPUOffersGuidanceAndAnExplicitCPUChoice(t *testing.T) {
	s := newScreen(t)
	s.rig.GPUReport = gpu.Report{Adapters: []gpu.Adapter{}, Recommendation: gpu.Recommendation{Guidance: "対応する GPU が見つかりません。外部の ComfyUI を使ってください", CPUAvailable: true}}
	body := s.get(paths.ModelManagerPart("engine", "gpu")).Body.String()
	assert.Contains(t, body, `data-testid="mm-gpu-none"`)
	assert.Contains(t, body, `data-testid="mm-gpu-guidance"`)
	assert.Contains(t, body, `name="cpu"`, "the CPU is an explicit opt-in")
	assert.NotContains(t, body, `data-testid="mm-gpu-recommended"`)

	rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "no GPU and no CPU choice installs nothing")
	assert.Contains(t, rec.Body.String(), "対応する GPU が見つかりません")
}

func TestInstallEngine_ShowsProgressThenTheInstalledCardAndEnablesTheCatalog(t *testing.T) {
	s := newScreen(t)
	rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"nvidia"}})
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, `data-testid="mm-progress"`)
	assert.Contains(t, body, `hx-trigger="every 2s"`, "the card polls itself while the install runs")
	assert.Contains(t, body, "中断する")
	job := jobID(t, body)
	assert.Contains(t, body, `hx-get="`+paths.ModelManagerPart("engine")+`?job=`+job+`"`)

	s.waitJob(job)
	poll := s.get(paths.ModelManagerPart("engine") + "?job=" + job)
	require.Equal(t, http.StatusOK, poll.Code)
	done := poll.Body.String()
	assert.Contains(t, done, "ComfyUI エンジンを導入しました")
	assert.Contains(t, done, `data-testid="mm-engine-state"`)
	assert.Contains(t, done, `data-state="stopped"`)
	assert.NotContains(t, done, `hx-trigger="every 2s"`, "polling stops once the install ended")
	assert.Contains(t, done, `id="mm-catalog"`, "the model list is drawn again, out of band")
	assert.Contains(t, done, `hx-swap-oob="true"`)
	assert.NotContains(t, s.page(), `data-testid="mm-catalog-blocked"`)
}

func TestEngine_StartStopAutoStartAndRemove(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()

	rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "start"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Eventually(t, func() bool {
		return strings.Contains(s.get(paths.ModelManagerPart("engine")).Body.String(), `data-state="running"`)
	}, 20*time.Second, 50*time.Millisecond)
	running := s.get(paths.ModelManagerPart("engine")).Body.String()
	assert.Contains(t, running, "127.0.0.1:")
	assert.Contains(t, running, "停止")

	rec = s.do(http.MethodPost, paths.ModelManagerPart("engine", "stop"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-state="stopped"`)

	rec = s.do(http.MethodPut, paths.ModelManagerPart("engine", "autostart"), url.Values{"on": {"1"}})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "オン")
	assert.True(t, s.rig.Svc.Engine().AutoStart)
	rec = s.do(http.MethodPut, paths.ModelManagerPart("engine", "autostart"), url.Values{"on": {"0"}})
	assert.False(t, s.rig.Svc.Engine().AutoStart)
	assert.Contains(t, rec.Body.String(), "オフ")

	rec = s.do(http.MethodDelete, paths.ModelManagerPart("engine"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "ComfyUI エンジンを削除しました")
	assert.False(t, s.rig.Svc.Engine().Installed)
	assert.Contains(t, rec.Body.String(), `data-testid="mm-engine-missing"`)
}

func TestEngine_RefusalsOfTheOSAndTheState(t *testing.T) {
	t.Run("not Windows: the card explains and every action is refused", func(t *testing.T) {
		s := newScreen(t, mmtest.NotWindows())
		body := s.page()
		assert.Contains(t, body, `data-testid="mm-engine-unsupported"`)
		assert.Contains(t, body, "外部の ComfyUI")
		assert.NotContains(t, body, `data-testid="mm-gpu"`)
		for _, rec := range []*http.Response{
			s.do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"nvidia"}}).Result(),
			s.do(http.MethodPost, paths.ModelManagerPart("engine", "start"), nil).Result(),
		} {
			assert.Equal(t, http.StatusConflict, rec.StatusCode)
		}
		rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "start"), nil)
		assert.Contains(t, rec.Body.String(), "Windows 版だけ")
	})
	t.Run("start without an engine", func(t *testing.T) {
		s := newScreen(t)
		rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "start"), nil)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "先に ComfyUI エンジンを導入してください")
	})
	t.Run("an unknown build", func(t *testing.T) {
		s := newScreen(t)
		rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"quantum"}})
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})
	t.Run("no room on the disk", func(t *testing.T) {
		s := newScreen(t)
		s.rig.Free = func(string) (uint64, error) { return 10, nil }
		rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"nvidia"}})
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "空き容量が足りません")
		assert.False(t, s.rig.Svc.Engine().Installed)
	})
}

func TestDetectComfyUI_ListsWhatAnsweredAndNeverStartsAnything(t *testing.T) {
	s := newScreen(t)
	rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "detect"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="mm-detect-none"`)
}

func TestCancelAnEngineInstallEndsItWithAMessageAndInstallingAgainWorks(t *testing.T) {
	s := newScreen(t)
	hold := s.rig.Host.Hold("/engine.7z")
	rec := s.do(http.MethodPost, paths.ModelManagerPart("engine", "install"), url.Values{"variant": {"nvidia"}})
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := jobID(t, rec.Body.String())
	s.rig.Start()
	hold.Reached(t)

	cancel := s.do(http.MethodPost, paths.ModelManagerPart("jobs", job, "cancel"), nil)
	require.Equal(t, http.StatusOK, cancel.Code, cancel.Body.String())
	assert.Contains(t, cancel.Body.String(), `id="mm-engine"`, "the card of the engine is answered, not the model list")
	s.waitJob(job)
	poll := s.get(paths.ModelManagerPart("engine") + "?job=" + job)
	assert.Contains(t, poll.Body.String(), "導入を中断しました")
	assert.False(t, s.rig.Svc.Engine().Installed)

	assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, paths.ModelManagerPart("jobs", "nope", "cancel"), nil).Code)
	hold.Release()
	s.rig.InstallEngine()
	assert.True(t, s.rig.Svc.Engine().Installed)
}
