package modelmgr_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

func installURL(id string) string { return paths.ModelManagerPart("models", id, "install") }

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
		assert.Contains(t, list, `data-testid="mm-low-disk"`, "the card says so")
		assert.Contains(t, list, "空きが足りません")
		assert.Contains(t, s.get(paths.ModelManagerPart("models", "m-a", "detail")).Body.String(), "空きが足りません。導入できません")
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

	detail := s.get(paths.ModelManagerPart("models", "m-b", "detail")).Body.String()
	assert.Contains(t, detail, "使うモデルが残る間は消えません", "the shared VAE is marked in the modal")

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

// card is the HTML of the card of the model in the list.
func card(t *testing.T, list, id string) string {
	t.Helper()
	_, rest, ok := strings.Cut(list, `<li id="mm-model-`+id+`"`)
	require.True(t, ok, id)
	card, _, _ := strings.Cut(rest, `<li id="mm-model-`)
	return card
}

func gatedForm() url.Values {
	return url.Values{"license_id": {mmtest.GatedLicense.ID}, "license_version": {mmtest.GatedLicense.Version}, "agree": {"1"}}
}

// A model behind the Hugging Face login (Issue #877) has the badge 「HF トークンが必要」 on its card; while no token is stored its detail modal says
// what to do, with the link to the card of the token, in the words the service refuses with (#954: the guidance moved from the card to the modal).
// The card of a model that needs no login has neither, and the card of the token says who it is for.
func TestCatalog_AModelBehindTheLoginHasABadgeAndTheGuidanceWhileThereIsNoToken(t *testing.T) {
	s := newScreen(t, mmtest.WithGatedModel(), mmtest.HFAuth())
	s.rig.InstallEngine()
	list := s.get(paths.ModelManagerPart("models")).Body.String()
	detailOf := func(id string) string { return s.get(paths.ModelManagerPart("models", id, "detail")).Body.String() }

	g := card(t, list, mmtest.GatedModelID)
	assert.Contains(t, g, `data-testid="mm-token-badge"`)
	assert.Contains(t, g, "HF トークンが必要")
	assert.NotContains(t, g, "mm-token-guide", "the list is short: the guidance is in the modal")
	gd := detailOf(mmtest.GatedModelID)
	assert.Contains(t, gd, `data-testid="mm-token-guide"`)
	assert.Contains(t, gd, hfguide.NoToken, "the same words as the refusal")
	assert.Contains(t, gd, `href="#mm-hf"`, "the link to the card of the token")
	assert.Contains(t, gd, "HF トークンが必要")
	description := strings.Index(gd, ">d</p>")
	assert.Greater(t, description, 0)
	assert.Less(t, description, strings.Index(gd, `data-testid="mm-token-guide"`), "the description comes first")
	a := card(t, list, "m-a")
	assert.NotContains(t, a, "mm-token-badge")
	assert.NotContains(t, detailOf("m-a"), "mm-token-guide")

	page := s.page()
	assert.Contains(t, page, `id="mm-hf"`)
	assert.Contains(t, page, "ログインや同意が要るモデル（例: LTX-2.5）")
	assert.NotContains(t, page, "いまのカタログのモデルは、トークンなしで取得できます")

	// once a token is stored the guidance goes, the badge stays
	rec := s.do(http.MethodPut, paths.ModelManagerPart("hf-token"), url.Values{"token": {s.rig.HF.Token}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), s.rig.HF.Token, "the token is never shown")
	g = card(t, s.get(paths.ModelManagerPart("models")).Body.String(), mmtest.GatedModelID)
	assert.Contains(t, g, "mm-token-badge")
	assert.NotContains(t, detailOf(mmtest.GatedModelID), "mm-token-guide")
}

// The install of such a model refuses before a job in the three ways with the guidance (422) and succeeds once the access was
// granted; the token is in none of the answers.
func TestInstallModel_AModelBehindTheLoginChecksTheTokenAndTheAccessBeforeAJob(t *testing.T) {
	s := newScreen(t, mmtest.WithGatedModel(), mmtest.HFAuth())
	s.rig.InstallEngine()
	token := s.rig.HF.Token
	var answers []string
	install := func() (int, string) {
		rec := s.do(http.MethodPost, installURL(mmtest.GatedModelID), gatedForm())
		answers = append(answers, rec.Body.String())
		return rec.Code, rec.Body.String()
	}

	code, body := install() // no token
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Contains(t, body, hfguide.NoToken)

	require.NoError(t, s.rig.Svc.SetHuggingFaceToken("hf_notthetokenthehubknows"))
	code, body = install() // a token the Hub does not accept
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Contains(t, body, hfguide.InvalidToken)

	require.NoError(t, s.rig.Svc.SetHuggingFaceToken(token))
	code, body = install() // a token without the access
	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.Contains(t, body, "ライセンスに同意してアクセスを申請")
	assert.Contains(t, body, s.rig.HF.URL+"/"+mmtest.GatedRepo, "the link to the page of the model")
	assert.Zero(t, s.rig.HF.Count("/cdn/"), "nothing was downloaded")

	s.rig.HF.Granted.Store(true)
	rec := s.do(http.MethodPost, installURL(mmtest.GatedModelID), gatedForm())
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	answers = append(answers, rec.Body.String())
	s.waitJob(jobID(t, rec.Body.String()))
	assert.FileExists(t, s.rig.ModelFile(mmtest.GatedFile))

	for _, a := range answers {
		assert.NotContains(t, a, token)
		assert.NotContains(t, a, "hf_notthetokenthehubknows")
	}
}
