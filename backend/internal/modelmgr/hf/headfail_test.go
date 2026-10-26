package hf_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
)

// The old commit's card has no tag, so the head decides; when the head cannot be read the repository's tag is unknown and the
// answer must be an error (never a Repo with NFAA=false), whatever the head's tag would have been.
func TestRevision_AnOlderCommitWhoseHeadCannotBeReadIsAnErrorNeverAnOpenGate(t *testing.T) {
	const old = "fedcba9876543210fedcba9876543210fedcba98"
	for name, head := range map[string]hftest.Repo{
		"404":       {HeadStatus: http.StatusNotFound},
		"500":       {HeadStatus: http.StatusInternalServerError},
		"malformed": {HeadMalformed: true},
	} {
		for _, nfaa := range []bool{true, false} {
			t.Run(name+"/nfaa="+strconv.FormatBool(nfaa), func(t *testing.T) {
				head.ID, head.Commit, head.OldCommit, head.NFAA = "o/m", commit, old, nfaa
				srv := hftest.New(t, head)
				c := newClient(srv, "")
				r, err := c.Revision(context.Background(), "o/m", old)
				require.Error(t, err)
				assert.Equal(t, hf.Repo{}, r)
				if head.HeadStatus != 0 {
					assert.True(t, hf.IsStatus(err, head.HeadStatus))
				}
				_, err = c.Revision(context.Background(), "o/m", commit) // named by its hash, the head's tag is read too
				require.Error(t, err)
			})
		}
	}
}

func TestRevision_ARenamedRepositoryWhoseHeadCannotBeReadIsAnError(t *testing.T) {
	const old = "fedcba9876543210fedcba9876543210fedcba98"
	srv := hftest.New(t, hftest.Repo{ID: "o/new", Commit: commit, OldCommit: old, NFAA: true, HeadStatus: http.StatusInternalServerError})
	srv.Rename("o/old", "o/new")
	_, err := newClient(srv, "").Revision(context.Background(), "o/old", old)
	require.Error(t, err)
	assert.True(t, hf.IsStatus(err, http.StatusInternalServerError))

	srv.Add(hftest.Repo{ID: "o/new", Commit: commit, OldCommit: old, NFAA: true}) // the head reads again: the renamed repository is adult
	r, err := newClient(srv, "").Revision(context.Background(), "o/old", old)
	require.NoError(t, err)
	assert.Equal(t, "o/new", r.ID)
	assert.True(t, r.NFAA)
}
