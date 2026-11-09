package nodes_test

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/nodecat"
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
	assert.NotContains(t, body, "needs opencv", "the long reason is not on the card (#1118): the popup of its button has it")
	assert.Equal(t, 1, strings.Count(body, `data-testid="mm-reason-open"`), "the node mam does not install has the button of the reason")
	assert.Contains(t, body, "このノードを使うモデル: Uses pack（未導入）")
	assert.NotContains(t, body, screentest.NodeCommitForTest, "the card does not carry the commit")
	assert.Equal(t, 1, strings.Count(body, `data-testid="mm-node-install"`), "the node mam does not install has no install button")
}

// #1118: the button on the card of a node that mam does not install opens a popup with the reason; a node that can be installed has no reason to give.
func TestNodes_TheReasonPopupOfANodeMamDoesNotInstallHasTheReason(t *testing.T) {
	s := screentest.NewWithNodes(t)
	rec := s.Get(nodePart("needs-pip", "blocked"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "needs opencv")
	assert.Contains(t, rec.Body.String(), `id="mam-modal-title"`)
	assert.Contains(t, rec.Body.String(), `data-modal-close`)
	assert.NotContains(t, rec.Body.String(), `hx-post=`, "the popup cannot install")
	assert.Equal(t, http.StatusNotFound, s.Get(nodePart("pack", "blocked")).Code)
	assert.Equal(t, http.StatusNotFound, s.Get(nodePart("nope", "blocked")).Code)
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

var modalDesc = regexp.MustCompile(`id="mam-modal-desc"[^>]*>([^<]*)</p>`)

// The description of a node is in its detail modal and nowhere on the card: for every node of the allowlist, the section, the list and the card
// a poll swaps in carry neither the element nor the text of the description, and the modal says it as its description (#mam-modal-desc, which modal.ts
// points aria-describedby at when the modal opens). A node that is not in the allowlist gets the modal of the reason only (#1103).
func TestNodes_TheDescriptionIsInTheModalAndNotOnTheCard(t *testing.T) {
	s := screentest.NewWithNodes(t)
	all := nodecat.Default()
	s.Nodes.Nodes.Catalog = all
	s.Nodes.Engine.Needing = nil
	require.Len(t, all.Nodes, 5, "the allowlist has five nodes")

	page := s.PageOf(paths.MMCustomNodes)
	list := s.Get(paths.ModelManagerPart("nodes")).Body.String()
	require.Equal(t, len(all.Nodes), strings.Count(page, `data-testid="mm-node"`))
	for _, n := range all.Nodes {
		cards := map[string]string{"the page": page, "the list": list, "the card": s.Get(nodePart(n.ID)).Body.String()}
		for name, body := range cards {
			assert.NotContains(t, body, "mm-node-description", "%s of %s", name, n.ID)
			assert.NotContains(t, body, n.Description, "%s of %s", name, n.ID)
			assert.NotContains(t, body, html.EscapeString(n.Description), "%s of %s", name, n.ID)
		}

		rec := s.Get(nodePart(n.ID, "detail"))
		require.Equal(t, http.StatusOK, rec.Code, n.ID)
		m := modalDesc.FindStringSubmatch(rec.Body.String())
		require.Len(t, m, 2, "the modal of %s has a description", n.ID)
		assert.Equal(t, n.Description, html.UnescapeString(m[1]), n.ID)
	}

	rec := s.Get(nodePart("nope", "detail"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), "mm-node-detail")
}
