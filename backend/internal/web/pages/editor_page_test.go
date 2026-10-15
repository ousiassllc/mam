package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/editorpanel"
	"github.com/ousiassllc/mam/backend/internal/web/pages"
)

// Every stage action of the editor tells the server which Branch it works on through hx-vals; the
// trunk sends an empty branch_id rather than none (atoms.HxVals, #568).
func TestEditorPage_StageActionsSendTheBranchInHxVals(t *testing.T) {
	for branch, want := range map[string]string{"": `{&#34;branch_id&#34;:&#34;&#34;}`, "b1": `{&#34;branch_id&#34;:&#34;b1&#34;}`} {
		var sb strings.Builder
		d := editorpanel.Data{Project: domain.Project{ID: "p", State: domain.StateAudio}, BranchID: branch}

		require.NoError(t, pages.EditorPage("tok", d).Render(context.Background(), &sb))

		assert.Contains(t, sb.String(), `hx-vals="`+want+`"`, branch)
	}
}
