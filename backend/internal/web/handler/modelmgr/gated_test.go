package modelmgr_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/hfguide"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// card is the HTML of the card of the model in the list.
func card(t *testing.T, list, id string) string {
	t.Helper()
	_, rest, ok := strings.Cut(list, `data-model="`+id+`"`)
	require.True(t, ok, id)
	card, _, _ := strings.Cut(rest, `data-testid="mm-model"`)
	return card
}

func gatedForm() url.Values {
	return url.Values{"license_id": {mmtest.GatedLicense.ID}, "license_version": {mmtest.GatedLicense.Version}, "agree": {"1"}}
}

// A model behind the Hugging Face login (Issue #877) has the badge 「HF トークンが必要」 on its card; while no token is stored the
// card says what to do, with the link to the card of the token, in the words the service refuses with. The card of a model that
// needs no login has neither, and the card of the token says who it is for.
func TestCatalog_AModelBehindTheLoginHasABadgeAndTheGuidanceWhileThereIsNoToken(t *testing.T) {
	s := newScreen(t, mmtest.WithGatedModel(), mmtest.HFAuth())
	s.rig.InstallEngine()
	list := s.get(paths.ModelManagerPart("models")).Body.String()

	g := card(t, list, mmtest.GatedModelID)
	assert.Contains(t, g, `data-testid="mm-token-badge"`)
	assert.Contains(t, g, "HF トークンが必要")
	assert.Contains(t, g, `data-testid="mm-token-guide"`)
	assert.Contains(t, g, hfguide.NoToken, "the same words as the refusal")
	assert.Contains(t, g, `href="#mm-hf"`, "the link to the card of the token")
	// the guidance is beside the install button (the same group of actions), not under the description
	button, guide := strings.Index(g, `data-testid="mm-consent-open"`), strings.Index(g, `data-testid="mm-token-guide"`)
	assert.Greater(t, button, 0)
	assert.Greater(t, guide, button, "after the button")
	assert.Less(t, guide, strings.Index(g, `data-testid="mm-model-meta"`), "before the facts of the card, where the description ends")
	description := strings.Index(g, ">d</p>")
	assert.Greater(t, description, 0)
	assert.Less(t, description, button, "the description comes first")
	a := card(t, list, "m-a")
	assert.NotContains(t, a, "mm-token-badge")
	assert.NotContains(t, a, "mm-token-guide")

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
	assert.NotContains(t, g, "mm-token-guide")
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

	s.rig.HF.Granted = true
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
