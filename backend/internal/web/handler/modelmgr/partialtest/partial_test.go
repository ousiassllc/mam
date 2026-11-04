// Package partialtest_test tests the model manager screen for a model that is installed without its optional files (#1025): the badge on the card, and in the
// detail modal the note, the files marked optional and the button that fetches them. It is a package of its own because the directory of the handlers is full (linterly).
package partialtest_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
	"github.com/ousiassllc/mam/backend/internal/web/screentest"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	mmtest.Main(m)
}

// card is the card of the model on the list (the <li> that carries data-model="id"), so that an assertion does not read another model's.
func card(t *testing.T, list, id string) string {
	t.Helper()
	i := strings.Index(list, `data-model="`+id+`"`)
	require.NotEqual(t, -1, i, "the model is on the list")
	rest := list[i:]
	if j := strings.Index(rest, "</li>"); j != -1 {
		rest = rest[:j]
	}
	return rest
}

// The required files are in place and the optional LoRA was deleted: the card says "導入済み" and carries the partial badge and the button that fetches the missing
// file; the detail modal has the note, the file marked "（任意）" with its missing state. With every file in place none of them is on the screen.
func TestAnInstalledModelWithoutItsOptionalFileShowsTheBadgeTheNoteAndTheButton(t *testing.T) {
	s := screentest.New(t, mmtest.WithOptionalModel())
	s.Rig.InstallEngine()
	require.Equal(t, "succeeded", string(s.Rig.InstallModel(mmtest.OptionalModelID).State))

	full := s.Get(paths.ModelManagerPart("models")).Body.String()
	assert.NotContains(t, card(t, full, mmtest.OptionalModelID), "mm-partial-badge")
	assert.NotContains(t, card(t, full, mmtest.OptionalModelID), "任意ファイルを取得")
	assert.NotContains(t, s.Get(paths.ModelManagerPart("models", mmtest.OptionalModelID, "detail")).Body.String(), "mm-partial-note")

	require.NoError(t, os.Remove(s.Rig.ModelFile(mmtest.LoraD)))

	c := card(t, s.Get(paths.ModelManagerPart("models")).Body.String(), mmtest.OptionalModelID)
	assert.Contains(t, c, `data-state="installed"`, "still installed: the required files are there")
	assert.Contains(t, c, `data-testid="mm-partial-badge"`)
	assert.Contains(t, c, "一部未導入（任意ファイル）")
	assert.Contains(t, c, "任意ファイルを取得", "the button that fetches the missing file")
	assert.Contains(t, c, `hx-post="`+paths.ModelManagerPart("models", mmtest.OptionalModelID, "install")+`"`)

	detail := s.Get(paths.ModelManagerPart("models", mmtest.OptionalModelID, "detail")).Body.String()
	assert.Contains(t, detail, `data-testid="mm-partial-note"`)
	assert.Contains(t, detail, mmtest.LoraD.Name, "the note names the missing file")
	assert.Contains(t, detail, `data-testid="mm-file-optional"`)
	assert.Contains(t, detail, "（任意）")
	assert.Equal(t, 1, strings.Count(detail, `data-testid="mm-file-optional"`), "only the LoRA is optional")
}

// While the missing file is fetched the model is still installed (it can be used): its card keeps the state "導入済み" and shows the progress and the button that
// stops the download, not the install button.
func TestFetchingTheOptionalFileKeepsTheCardInstalledAndShowsTheJob(t *testing.T) {
	s := screentest.New(t, mmtest.WithOptionalModel())
	s.Rig.InstallEngine()
	require.Equal(t, "succeeded", string(s.Rig.InstallModel(mmtest.OptionalModelID).State))
	require.NoError(t, os.Remove(s.Rig.ModelFile(mmtest.LoraD)))
	hold := s.Rig.Host.Hold("/loras/" + mmtest.LoraD.Name)
	t.Cleanup(hold.Release)

	rec := s.Do(http.MethodPost, paths.ModelManagerPart("models", mmtest.OptionalModelID, "install"), nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	hold.Reached(t)

	c := card(t, s.Get(paths.ModelManagerPart("models")).Body.String(), mmtest.OptionalModelID)
	assert.Contains(t, c, `data-state="installed"`)
	assert.Contains(t, c, "中断")
	assert.NotContains(t, c, `hx-post="`+paths.ModelManagerPart("models", mmtest.OptionalModelID, "install")+`"`, "the fetch is running: no second install button")
	assert.Contains(t, c, `hx-post="`+paths.ModelManagerPart("jobs")+"/", "the cancel button of the job")
}
