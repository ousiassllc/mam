package hfimport_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

const commit = "0123456789abcdef0123456789abcdef01234567"

var (
	sdxlBody = hftest.Safetensors([]string{"model.diffusion_model.a", "conditioner.embedders.1.model.b", "first_stage_model.c"}, nil, 4000)
	loraBody = hftest.Safetensors([]string{"lora_unet_a.lora_down.weight", "lora_unet_a.alpha"}, nil, 500)
)

func repo() hftest.Repo {
	return hftest.Repo{ID: "o/m", Commit: commit, License: "openrail++", Downloads: 120, Likes: 7, PipelineTag: "text-to-image", Files: []hftest.File{
		{Path: "pony.safetensors", Body: sdxlBody},
		{Path: "style-lora.safetensors", Body: loraBody},
		{Path: "m.Q4_K_M.gguf", Body: []byte("GGUF....")},
		{Path: "old.ckpt", Body: []byte("pickle..")},
	}}
}

func newScreen(t *testing.T, repos ...hftest.Repo) *screentest.Screen {
	t.Helper()
	s := screentest.New(t)
	for _, r := range repos {
		s.Rig.HF.Add(r)
	}
	return s
}

func hfURL(q string) string {
	return paths.ModelManagerPart("hf") + "?" + url.Values{"q": {q}}.Encode()
}

func install(path string, extra url.Values) url.Values {
	f := url.Values{"repo": {"o/m"}, "commit": {commit}, "path": {path}, "terms_version": {hfplan.TermsVersion}}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func TestThePageHasTheImportSectionAndTheTokenStaysOutOfIt(t *testing.T) {
	s := newScreen(t)
	body := s.Page()
	assert.Contains(t, body, `data-testid="mm-hfi-form"`)
	assert.Contains(t, body, `data-testid="mm-hfi-none"`)
	assert.Contains(t, body, `id="mm-hfi-msg"`)
	assert.Contains(t, body, `maxlength="`+"512"+`"`, "the query field is limited as the service limits it")
}

func TestAKeywordSearchListsTheRepositoriesAndAnEmptyOneIsRefused(t *testing.T) {
	s := newScreen(t, repo())
	rec := s.Get(hfURL("m"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-repo="o/m"`)
	assert.Contains(t, rec.Body.String(), "ダウンロード 120")

	rec = s.Get(hfURL("zzz-nothing"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "見つかりませんでした")

	rec = s.Get(hfURL("  "))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestTheFilesOfARepositoryComeFromAnIdOrAUrlAndShowWhyAFileCannotBeInstalled(t *testing.T) {
	s := newScreen(t, repo())
	for _, in := range []string{"o/m", "https://huggingface.co/o/m", "https://huggingface.co/o/m/blob/main/pony.safetensors"} {
		rec := s.Get(hfURL(in))
		require.Equal(t, http.StatusOK, rec.Code, in)
		body := rec.Body.String()
		assert.Contains(t, body, `data-commit="`+commit+`"`, in)
		assert.Contains(t, body, "openrail++", in)
		assert.Contains(t, body, hfplan.Terms, in)
		assert.Contains(t, body, `name="terms_version" value="`+hfplan.TermsVersion+`"`, in)
		assert.Contains(t, body, `data-path="pony.safetensors" data-installable="true"`, in)
		assert.Contains(t, body, `data-path="m.Q4_K_M.gguf" data-installable="false"`, in)
		assert.Contains(t, body, hfplan.ReasonGGUF, in)
		assert.Contains(t, body, `data-path="old.ckpt" data-installable="false"`, in)
		assert.Contains(t, body, hfplan.ReasonPickle, in)
		assert.NotContains(t, body, `hf_testtoken`, in)
	}
	assert.Contains(t, s.Get(hfURL("https://huggingface.co/o/m/blob/main/pony.safetensors")).Body.String(), "選択中のファイル")

	rec := s.Get(hfURL("https://hf.co/o/m"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "the short URL is not taken")
	rec = s.Get(hfURL("o/nothing-here"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "a repository that is not there is said so")
	rec = s.Get(hfURL("http://huggingface.co/o/m"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "only https")
	rec = s.Get(hfURL("https://example.com/o/m"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "only Hugging Face")
}

func TestInstallNeedsTheTermsAndAFileThatCanBeInstalled(t *testing.T) {
	s := newScreen(t, repo())
	s.Rig.InstallEngine()

	rec := s.Do(http.MethodPost, paths.ModelManagerPart("hf", "install"), install("pony.safetensors", nil))
	assert.Equal(t, http.StatusConflict, rec.Code, "no confirmation of the terms")
	assert.Equal(t, "#mm-hfi-msg", rec.Header().Get("HX-Retarget"))

	rec = s.Do(http.MethodPost, paths.ModelManagerPart("hf", "install"), install("m.Q4_K_M.gguf", url.Values{"agree": {"1"}}))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), hfplan.ReasonGGUF)

	rec = s.Do(http.MethodPost, paths.ModelManagerPart("hf", "install"), install("old.ckpt", url.Values{"agree": {"1"}}))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), hfplan.ReasonPickle)

	rec = s.Do(http.MethodPost, paths.ModelManagerPart("hf", "install"), install("pony.safetensors", url.Values{"agree": {"1"}, "terms_version": {"0"}}))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "a wording of the terms that was not shown")
	assert.Empty(t, s.Rig.Svc.UserModels())
}

func TestInstallingAFileShowsProgressThenTheModelAndDeletingItRemovesIt(t *testing.T) {
	s := newScreen(t, repo())
	s.Rig.InstallEngine()

	rec := s.Do(http.MethodPost, paths.ModelManagerPart("hf", "install"), install("pony.safetensors", url.Values{"agree": {"1"}}))
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="mm-hfi-job"`)
	assert.Contains(t, rec.Body.String(), "導入を始めました")
	id := screentest.JobID(t, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`, "the list polls while it runs")

	s.WaitJob(id)
	rec = s.Get(paths.ModelManagerPart("hf", "models") + "?jobs=" + id)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `data-path="models/checkpoints/pony.safetensors"`)
	assert.Contains(t, body, "導入しました")
	assert.Contains(t, body, `data-testid="mm-trial"`, "the trial choices are drawn again with the new model")
	assert.NotContains(t, body, `hx-trigger="every 2s"`)
	assert.Contains(t, s.Page(), `data-path="models/checkpoints/pony.safetensors"`)

	// a second terms confirmation is not asked for the same repository and commit
	repoBody := s.Get(hfURL("o/m")).Body.String()
	assert.NotContains(t, repoBody, `data-testid="mm-hfi-agree"`)
	assert.Contains(t, repoBody, "導入済み")

	rec = s.Do(http.MethodDelete, paths.ModelManagerPart("hf", "models")+"?path="+url.QueryEscape("models/checkpoints/pony.safetensors"), nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `data-path="models/checkpoints/pony.safetensors"`)
	assert.Empty(t, s.Rig.Svc.UserModels())

	rec = s.Do(http.MethodDelete, paths.ModelManagerPart("hf", "models")+"?path="+url.QueryEscape("models/checkpoints/none.safetensors"), nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "nothing is installed there (the same answer as the catalog gives)")
}

func TestARepositoryForAdultsIsHiddenUntilTheAgeGateIsOpen(t *testing.T) {
	adult := repo()
	adult.ID, adult.NFAA = "o/adult", true
	s := newScreen(t, adult)

	rec := s.Get(hfURL("adult"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `data-repo="o/adult"`, "left out of the search")
	assert.Equal(t, http.StatusConflict, s.Get(hfURL("o/adult")).Code)

	s.Rig.ConfirmAdult()
	assert.Contains(t, s.Get(hfURL("adult")).Body.String(), `data-repo="o/adult"`)
	rec = s.Get(hfURL("o/adult"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="mm-hfi-nfaa"`)
}

func TestAGatedRepositoryNeedsTheTokenAndAccessAndTheScreenSaysSo(t *testing.T) {
	gated := repo()
	gated.ID, gated.Gated = "o/gated", true
	s := newScreen(t, gated)
	s.Rig.InstallEngine()

	body := s.Get(hfURL("o/gated")).Body.String()
	assert.Contains(t, body, `data-testid="mm-hfi-gated-notice"`)

	form := install("pony.safetensors", url.Values{"agree": {"1"}, "repo": {"o/gated"}})
	rec := s.Do(http.MethodPost, paths.ModelManagerPart("hf", "install"), form)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "gated")
	assert.Empty(t, s.Rig.Svc.UserModels())
}
