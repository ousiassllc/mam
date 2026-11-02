package rolenotice_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/gentest/rolenotice"
)

var (
	notes  = []rolenotice.Note{{Model: "Chroma1-HD", Roles: []string{"顔だけ"}}, {Model: "FLUX.1 dev", Roles: []string{"顔だけ", "画風だけ"}}}
	fields = []rolenotice.Field{{Name: "media", Value: "image"}, {Name: "estimate_id", Value: "abc"}, {Name: "confirmed_total_usd", Value: "0"}}
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, c.Render(context.Background(), &sb))
	return sb.String()
}

// The body names every model with its roles and has two choices and no other: "戻る" (the close button of the modal) and "このまま実行"
// (the submit of the run), whose form carries the fields of the confirmation back.
func TestFrame_NamesEachModelAndHasTwoChoices(t *testing.T) {
	html := render(t, rolenotice.Frame(notes, fields))

	assert.Contains(t, html, "<strong>Chroma1-HD</strong> は「顔だけ」に対応していないので、全体の参照として使います。構図や服も寄ります")
	assert.Contains(t, html, "<strong>FLUX.1 dev</strong> は「顔だけ」「画風だけ」に対応していないので")
	assert.Equal(t, 2, strings.Count(html, "<button"))
	assert.Contains(t, html, "data-modal-close")
	assert.Contains(t, html, ">戻る<")
	assert.Contains(t, html, ">このまま実行<")
	assert.Contains(t, html, `hx-post="/generation-test/run"`)
	assert.Contains(t, html, `<input type="hidden" name="estimate_id" value="abc">`)
	assert.Contains(t, html, `id="mam-modal-title"`)
}

// The run button submits the form when no model has anything to say, and opens the modal with a POST of the form when one has.
func TestRunAttrs_SubmitsOrOpensTheModal(t *testing.T) {
	plain := rolenotice.RunAttrs(false, 0)
	assert.Equal(t, "submit", plain["type"])
	assert.NotContains(t, plain, "hx-post")

	open := rolenotice.RunAttrs(true, 1)
	assert.Equal(t, "button", open["type"])
	assert.Equal(t, "/generation-test/run-notice", open["hx-post"])
	assert.Equal(t, "#mam-modal-body", open["hx-target"])
	assert.NotContains(t, open, "hx-get")
	assert.Equal(t, true, open["disabled"])
}

// The wtest static/src/components/gentest/role-notice.wtest.ts opens these files: the confirmation whose run button opens the modal,
// and the modal's body. Write them again with `MAM_UPDATE_FIXTURES=1 go test ./backend/internal/web/organisms/gentest/rolenotice`.
func TestFixtures_AreWhatTheComponentsRender(t *testing.T) {
	hidden := []gentest.Hidden{{Name: "media", Value: "image"}, {Name: "estimate_id", Value: "abc"}, {Name: "confirmed_total_usd", Value: "0"}}
	for name, c := range map[string]templ.Component{
		"gentest-role-confirm.html": gentest.ConfirmBox(gentest.Confirm{Items: 2, Total: "$0.00", Hidden: hidden, RoleNotes: notes}),
		"gentest-role-notice.html":  rolenotice.Frame(notes, fields),
	} {
		file := filepath.Join("..", "..", "..", "..", "..", "..", "static", "src", "components", "testdata", name)
		got := render(t, c)
		if os.Getenv("MAM_UPDATE_FIXTURES") != "" {
			require.NoError(t, os.WriteFile(file, []byte(got), 0o600)) // #nosec G306 -- a test fixture in the repository
			continue
		}
		want, err := os.ReadFile(file) // #nosec G304 -- a fixed path inside the repository
		require.NoError(t, err, "write it with MAM_UPDATE_FIXTURES=1")
		assert.Equal(t, string(want), got, "the component changed: write the fixture again with MAM_UPDATE_FIXTURES=1")
	}
}
