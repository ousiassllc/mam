package modelmgr_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func installURL(id string) string { return paths.ModelManagerPart("models", id, "install") }

func TestCatalog_ShowsKindSizeVRAMLicenseCommercialAndState(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	body := s.get(paths.ModelManagerPart("models")).Body.String()
	assert.Contains(t, body, `data-model="m-a"`)
	assert.Contains(t, body, "画像")
	assert.Contains(t, body, "動画")
	assert.Contains(t, body, "195 KiB", "the size of a.safetensors")
	assert.Contains(t, body, "8 GB 以上（mam の推定。GPU では未確認）")
	assert.Contains(t, body, `data-testid="mm-commercial"`)
	assert.Contains(t, body, "不可（非商用）", "the commercial flag of m-c")
	assert.Contains(t, body, "ライセンスの同意が必要")
	assert.Contains(t, body, "構造テストのみ・GPU 実機未検証")
	assert.Contains(t, body, "ライセンスを確認して導入", "a model that needs consent opens the dialog, it does not install")
	assert.NotContains(t, body, `hx-post="`+installURL("m-c")+`"`)
}

// A model is a card in a grid, not a row of a divided list (model-manager-screen.md "モデルの一覧のカード"): the list is a
// <ul role="list">, each card is an <li> with the name as its h3, the state as a Badge with words, the buttons in a
// shrink-0 box next to the name and the description, then the meta, and the file list.
func TestCatalog_EachModelIsACardWithAnH3AndShrinkZeroActions(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	body := s.get(paths.ModelManagerPart("models")).Body.String()

	assert.Regexp(t, `<ul role="list" class="[^"]*\bgrid\b[^"]*" data-testid="mm-models">`, body)
	assert.Regexp(t, `<li class="[^"]*rounded-lg border border-neutral-800 bg-neutral-900[^"]*" data-testid="mm-model"`, body, "the surface of a card")
	assert.NotContains(t, body, "divide-y", "the models are not a divided list of rows")

	cards := strings.Split(body, `data-testid="mm-model" data-model="`)[1:]
	require.GreaterOrEqual(t, len(cards), 2, "several models are drawn")
	for _, card := range cards {
		id := card[:strings.Index(card, `"`)]
		assert.Regexp(t, `<h3 class="[^"]*">[^<]+</h3>`, card, "%s: the name is the heading of the card", id)
		assert.Contains(t, card, `data-testid="mm-model-state"`, id)
		assert.Regexp(t, `(?s)<div class="flex shrink-0 [^"]*">\s*<button`, card, "%s: the buttons stay in a shrink-0 box", id)
		for _, testID := range []string{"mm-model-meta", "mm-vram", "mm-commercial", "mm-files"} {
			assert.Contains(t, card, `data-testid="`+testID+`"`, "%s: the card keeps %s", id, testID)
		}
		assert.Contains(t, card, "<details", id)
		assert.NotContains(t, card, `data-testid="mm-model"`, id)
	}
}

func TestInstallModel_RunsAsAJobWithProgressAndThenIsInstalled(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	rec := s.do(http.MethodPost, installURL("m-a"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	body := rec.Body.String()
	assert.Contains(t, body, "導入を始めました")
	assert.Contains(t, body, `hx-trigger="every 2s"`)
	assert.Contains(t, body, `data-state="installing"`)
	job := jobID(t, body)

	s.waitJob(job)
	done := s.get(paths.ModelManagerPart("models") + "?jobs=" + job)
	require.Equal(t, http.StatusOK, done.Code)
	assert.Contains(t, done.Body.String(), "「A」を導入しました")
	assert.Contains(t, done.Body.String(), `data-state="installed"`)
	assert.NotContains(t, done.Body.String(), `hx-trigger="every 2s"`)
	assert.Contains(t, done.Body.String(), `id="mm-trial"`, "the trial choices are refreshed out of band")
	assert.FileExists(t, s.rig.ModelFile(mmtest.CkptA))
	assert.Contains(t, done.Body.String(), "comfyui:bundled-comfyui/wf-a")

	again := s.do(http.MethodPost, installURL("m-a"), nil)
	assert.Equal(t, http.StatusConflict, again.Code)
	assert.Contains(t, again.Body.String(), "導入済み")
}

func TestInstallModel_RefusalsBeforeAnythingIsDownloaded(t *testing.T) {
	t.Run("no engine", func(t *testing.T) {
		s := newScreen(t)
		rec := s.do(http.MethodPost, installURL("m-a"), nil)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "先に ComfyUI エンジンを導入してください")
		assert.Zero(t, s.rig.Host.Requests())
	})
	t.Run("unknown model", func(t *testing.T) {
		s := newScreen(t)
		s.rig.InstallEngine()
		assert.Equal(t, http.StatusNotFound, s.do(http.MethodPost, installURL("nope"), nil).Code)
	})
	t.Run("not enough room", func(t *testing.T) {
		s := newScreen(t)
		s.rig.InstallEngine()
		s.rig.Free = func(string) (uint64, error) { return 1000, nil }
		rec := s.do(http.MethodPost, installURL("m-a"), nil)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "空き容量が足りません")
		list := s.get(paths.ModelManagerPart("models")).Body.String()
		assert.Contains(t, list, "空きが足りません。導入できません")
	})
	t.Run("pinned model file", func(t *testing.T) {
		s := newScreen(t, mmtest.Pinned())
		s.rig.InstallEngine()
		rec := s.do(http.MethodPost, installURL("m-a"), nil)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "--model-config")
		assert.Contains(t, s.page(), "固定されているため、モデルを導入・削除できません")
	})
	t.Run("not Windows", func(t *testing.T) {
		s := newScreen(t, mmtest.NotWindows())
		rec := s.do(http.MethodPost, installURL("m-a"), nil)
		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Contains(t, rec.Body.String(), "Windows 版だけ")
	})
}

func TestConsent_TheDialogShowsTheLicenseAndTheServerEnforcesTheAcceptance(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()

	dlg := s.get(paths.ModelManagerPart("models", "m-c", "consent"))
	require.Equal(t, http.StatusOK, dlg.Code)
	d := dlg.Body.String()
	assert.Contains(t, d, `data-testid="mm-consent"`)
	assert.Contains(t, d, "summary of non-commercial", "the license summary")
	assert.Contains(t, d, "https://example.com/non-commercial", "the license URL")
	assert.Contains(t, d, "不可（非商用）")
	assert.Contains(t, d, "EU・韓国", "the region note")
	assert.Contains(t, d, `name="license_id" value="non-commercial"`)
	assert.Contains(t, d, `name="agree"`)
	assert.Equal(t, http.StatusNotFound, s.get(paths.ModelManagerPart("models", "m-a", "consent")).Code, "m-a needs no acceptance")

	// a request that skips the dialog is refused by the service: no job, the dialog is drawn with the reason
	skip := s.do(http.MethodPost, installURL("m-c"), nil)
	assert.Equal(t, http.StatusConflict, skip.Code)
	assert.Equal(t, "#mm-dialog", skip.Header().Get("HX-Retarget"))
	assert.Contains(t, skip.Body.String(), "ライセンスに同意してから導入してください")
	assert.Zero(t, s.rig.Host.Requests()-1, "nothing but the engine was downloaded") // the engine archive: 1 request
	m, err := s.rig.Svc.Model("m-c")
	require.NoError(t, err)
	assert.False(t, m.Accepted)

	// an acceptance of another license or version is refused (422) and not recorded
	wrong := s.do(http.MethodPost, installURL("m-c"), url.Values{"agree": {"1"}, "license_id": {"apache-2.0"}, "license_version": {"1"}})
	assert.Equal(t, http.StatusUnprocessableEntity, wrong.Code)
	m, _ = s.rig.Svc.Model("m-c")
	assert.False(t, m.Accepted)

	ok := s.do(http.MethodPost, installURL("m-c"), url.Values{"agree": {"1"}, "license_id": {"non-commercial"}, "license_version": {"1"}})
	require.Equal(t, http.StatusAccepted, ok.Code, ok.Body.String())
	assert.Contains(t, ok.Body.String(), `id="mm-dialog" hx-swap-oob="innerHTML"`, "the dialog is closed")
	m, _ = s.rig.Svc.Model("m-c")
	assert.True(t, m.Accepted)
	s.waitJob(jobID(t, ok.Body.String()))
	assert.Equal(t, "installed", string(s.rig.Svc.Catalog()[2].State))
}

func TestDeleteModel_SharedFilesStayWhileAnotherModelUsesThem(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	require.Equal(t, "succeeded", string(s.rig.InstallModel("m-b").State))
	require.Equal(t, "succeeded", string(s.rig.InstallModel("m-c").State))

	list := s.get(paths.ModelManagerPart("models")).Body.String()
	assert.Contains(t, list, "使うモデルが残る間は消えません", "the shared VAE is marked")

	rec := s.do(http.MethodDelete, paths.ModelManagerPart("models", "m-b"), nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "モデルを削除しました")
	assert.NoFileExists(t, s.rig.ModelFile(mmtest.UnetB))
	assert.FileExists(t, s.rig.ModelFile(mmtest.VaeBC), "m-c still uses the shared VAE")

	rec = s.do(http.MethodDelete, paths.ModelManagerPart("models", "m-c"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NoFileExists(t, s.rig.ModelFile(mmtest.VaeBC))

	again := s.do(http.MethodDelete, paths.ModelManagerPart("models", "m-c"), nil)
	assert.Equal(t, http.StatusConflict, again.Code)
	assert.Contains(t, again.Body.String(), "導入されていません")
	assert.Equal(t, http.StatusNotFound, s.do(http.MethodDelete, paths.ModelManagerPart("models", "nope"), nil).Code)
}

func TestCancelAModelInstallKeepsThePartialFilesAndAnotherInstallResumes(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	hold := s.rig.Host.Hold("/" + mmtest.CkptA.Folder + "/" + mmtest.CkptA.Name)
	rec := s.do(http.MethodPost, installURL("m-a"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code)
	job := jobID(t, rec.Body.String())
	s.rig.Start()
	hold.Reached(t)

	cancel := s.do(http.MethodPost, paths.ModelManagerPart("jobs", job, "cancel"), nil)
	require.Equal(t, http.StatusOK, cancel.Code, cancel.Body.String())
	assert.Contains(t, cancel.Body.String(), "導入を中断しています")
	hold.Release()
	s.waitJob(job)
	st, err := s.rig.Svc.JobStatus(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, "failed", string(st.State))
	assert.Contains(t, st.Error, "導入を中断しました")
	assert.NoFileExists(t, s.rig.ModelFile(mmtest.CkptA), "nothing was installed")
	assert.Equal(t, "not_installed", string(s.rig.Svc.Catalog()[0].State))

	assert.Equal(t, "succeeded", string(s.rig.InstallModel("m-a").State), "installing again works")
}

func TestCSRF_AnActionWithoutTheTokenIsRefusedAndChangesNothing(t *testing.T) {
	s := newScreen(t)
	s.rig.InstallEngine()
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, installURL("m-a")},
		{http.MethodDelete, paths.ModelManagerPart("engine")},
		{http.MethodPost, paths.ModelManagerPart("engine", "stop")},
		{http.MethodPut, paths.ModelManagerPart("hf-token")},
		{http.MethodPost, paths.ModelManagerPart("trials")},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader("token=hf_x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(s.cookie) // the cookie without the header
		rec := s.send(req)
		assert.Equal(t, http.StatusForbidden, rec.Code, c.method+" "+c.path)
	}
	assert.True(t, s.rig.Svc.Engine().Installed)
	assert.Equal(t, "not_installed", string(s.rig.Svc.Catalog()[0].State))
}
