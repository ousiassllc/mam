package workflows_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/sidenav"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// The node screen and the workflow screen are in the side menu and mark their own entry as the current one (#915). They
// are the two entries that the route test of the menu (web/sidenavtest/sidenav_test.go) cannot reach: they exist only with the workflows service.
func TestSideMenuEntriesOfTheNodeAndWorkflowScreens(t *testing.T) {
	s := newScreen(t)
	for _, tc := range []struct {
		path string
		key  sidenav.Key
	}{{paths.Workflows(), sidenav.Workflows}, {paths.Nodes(), sidenav.Nodes}} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.AddCookie(s.cookie)
		rec := s.send(req)
		require.Equal(t, http.StatusOK, rec.Code, tc.path)
		assert.Contains(t, rec.Body.String(), `data-key="`+string(tc.key)+`" aria-current="page"`, tc.path)
		assert.Equal(t, 1, countOf(rec.Body.String(), `aria-current="page"`), "only the entry of the screen is current")
	}
}

func countOf(s, sub string) int { return len(strings.Split(s, sub)) - 1 }
