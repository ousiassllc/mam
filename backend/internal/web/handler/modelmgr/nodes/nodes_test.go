package nodes_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelnodes"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

func nodePart(id string, rest ...string) string {
	return paths.ModelManagerPart(append([]string{"nodes", id}, rest...)...)
}

// consent is the form the consent dialog sends.
func consent() url.Values {
	return url.Values{"agree": {"1"}, "license_id": {"gpl"}, "license_version": {"3.0"}, "notice_version": {modelnodes.NoticeVersion}}
}

// The page has a section of the allowlist: every node with its state and answer for commercial use, why a node is not installed, and the models that
// need it. Nothing of the pins is on the card; the modal has them.
func TestNodes_ThePageListsTheAllowlistWithTheAnswerForCommercialUseAndWhyOneCannotBeInstalled(t *testing.T) {
	s := screentest.NewWithNodes(t)
	body := s.PageOf(paths.MMCustomNodes)
	assert.Contains(t, body, `id="mm-nodes"`)
	assert.Equal(t, 2, strings.Count(body, `data-testid="mm-node"`), "a card for each node of the allowlist")
	assert.Contains(t, body, `data-node="pack" data-state="not_installed"`)
	assert.Contains(t, body, `data-node="needs-pip" data-state="blocked"`)
	assert.Contains(t, body, "導入できない理由: needs opencv")
	assert.Contains(t, body, "このノードを使うモデル: Uses pack（未導入）")
	assert.NotContains(t, body, screentest.NodeCommitForTest, "the card does not carry the commit")
	assert.Equal(t, 1, strings.Count(body, `data-testid="mm-node-install"`), "the node mam does not install has no install button")
}

func TestNodes_TheModalHasThePinsTheLicenseTheDependenciesAndTheReview(t *testing.T) {
	s := screentest.NewWithNodes(t)
	rec := s.Get(nodePart("pack", "detail"))
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, screentest.NodeCommitForTest)
	assert.Regexp(t, `data-testid="mm-sha256">[0-9a-f]{64}<`, body)
	assert.Contains(t, body, "GPL-3.0")
	assert.Contains(t, body, "https://example.com/gpl")
	assert.Contains(t, body, "torch")
	assert.Contains(t, body, "No network, no subprocess.")
	assert.Contains(t, body, "ComfyUI の中でそのまま実行されるプログラム")

	rec = s.Get(nodePart("needs-pip", "detail"))
	assert.Contains(t, rec.Body.String(), "導入できない理由: needs opencv")
	assert.Contains(t, rec.Body.String(), "不明（確認できていません）", "the answer for commercial use of a node that is not clear is not hidden")
	assert.Equal(t, http.StatusNotFound, s.Get(nodePart("nope", "detail")).Code)
}

// The install asks first: the dialog has the security notice and the license; a request without the agreement installs nothing and answers with the dialog
// again, and the engine is told to load nothing.
func TestNodes_InstallNeedsTheAgreementAndInstallsNothingWithoutIt(t *testing.T) {
	s := screentest.NewWithNodes(t)
	rec := s.Get(nodePart("pack", "consent"))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-testid="mm-node-consent-security"`)
	assert.Contains(t, rec.Body.String(), "GPL-3.0")
	assert.Contains(t, rec.Body.String(), screentest.NodeCommitForTest)

	rec = s.Do(http.MethodPost, nodePart("pack", "install"), url.Values{"license_id": {"gpl"}, "license_version": {"3.0"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Equal(t, "#mm-nodes-dialog", rec.Header().Get("HX-Retarget"))
	assert.Contains(t, rec.Body.String(), `data-testid="mm-node-consent-error"`)
	assert.Empty(t, s.Nodes.Nodes.Enabled())

	rec = s.Do(http.MethodPost, nodePart("pack", "install"), func() url.Values { v := consent(); v.Set("notice_version", "old"); return v }())
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "an acceptance of another version of the notice is no acceptance")
	assert.Empty(t, s.Nodes.Nodes.Enabled())
}

func TestNodes_InstallThenRemoveChangesWhatTheEngineIsToldToLoad(t *testing.T) {
	s := screentest.NewWithNodes(t)
	rec := s.Do(http.MethodPost, nodePart("pack", "install"), consent())
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `data-testid="mm-progress"`)
	assert.Contains(t, rec.Body.String(), `hx-trigger="every 2s"`, "the card polls while the install runs")
	job := screentest.JobID(t, rec.Body.String())
	st := s.Nodes.Wait(job)
	require.Equal(t, domain.JobSucceeded, st.State, st.Error)

	rec = s.Get(nodePart("pack") + "?job=" + job)
	assert.Contains(t, rec.Body.String(), `data-state="installed"`)
	assert.Contains(t, rec.Body.String(), "Pack を導入しました")
	assert.Equal(t, []string{"pack"}, s.Nodes.Nodes.Enabled())
	assert.Contains(t, s.PageOf(paths.MMCustomNodes), "このノードを使うモデル: Uses pack（未導入）")

	rec = s.Do(http.MethodDelete, nodePart("pack"), nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `data-state="not_installed"`)
	assert.Empty(t, s.Nodes.Nodes.Enabled())

	rec = s.Do(http.MethodDelete, nodePart("pack"), nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "a node that is not installed cannot be removed")
}

func TestNodes_ANodeMamDoesNotInstallIsRefusedWithTheReason(t *testing.T) {
	s := screentest.NewWithNodes(t)
	rec := s.Do(http.MethodPost, nodePart("needs-pip", "install"), consent())
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "needs opencv")
	assert.Empty(t, s.Nodes.Nodes.Enabled())
	assert.Equal(t, http.StatusNotFound, s.Do(http.MethodPost, nodePart("nope", "install"), consent()).Code)
}

// Without the bundled engine (another OS, not installed) no node installs, and the card says why rather than failing after the download.
func TestNodes_WithoutTheEngineTheInstallIsRefusedAtOnce(t *testing.T) {
	s := screentest.NewWithNodes(t)
	s.Nodes.Engine.View.Installed = false
	body := s.PageOf(paths.MMCustomNodes)
	assert.Contains(t, body, "先に ComfyUI エンジンを導入してください")
	rec := s.Do(http.MethodPost, nodePart("pack", "install"), consent())
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Empty(t, s.Nodes.Nodes.Enabled())
}
