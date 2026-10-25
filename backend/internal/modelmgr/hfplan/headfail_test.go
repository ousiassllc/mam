package hfplan_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hf/hftest"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/hfplan"
)

// When the head cannot be read, the tag the head carries is unknown: Browse fails closed, with or without the age gate open.
func TestBrowse_AnOlderCommitWhoseHeadCannotBeReadIsAnErrorEvenWithTheGateOpen(t *testing.T) {
	for name, set := range map[string]func(*hftest.Repo){
		"404":       func(r *hftest.Repo) { r.HeadStatus = http.StatusNotFound },
		"500":       func(r *hftest.Repo) { r.HeadStatus = http.StatusInternalServerError },
		"malformed": func(r *hftest.Repo) { r.HeadMalformed = true },
	} {
		t.Run(name, func(t *testing.T) {
			r := repo()
			r.OldCommit = oldCommit
			set(&r)
			srv := hftest.New(t, r)
			for _, adultOK := range []bool{false, true} {
				l, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/m", Kind: hf.KindTree, Rev: oldCommit}, hfplan.BrowseOptions{AdultOK: adultOK})
				require.Error(t, err, "adultOK=%v", adultOK)
				assert.NotErrorIs(t, err, hfplan.ErrAdult)
				assert.Empty(t, l.Files)
				assert.Empty(t, l.Repo.ID, "no Listing")
			}
			assert.Zero(t, srv.Count("/api/models/o/m/tree/"), "nothing of the repository is read")
		})
	}
}

func TestBrowse_ARenamedRepositoryWhoseHeadCannotBeReadIsAnError(t *testing.T) {
	r := repo()
	r.OldCommit, r.HeadStatus = oldCommit, http.StatusInternalServerError
	srv := hftest.New(t, r)
	srv.Rename("o/old", "o/m")
	l, err := hfplan.Browse(context.Background(), client(srv, ""), hf.Ref{Repo: "o/old", Kind: hf.KindTree, Rev: oldCommit}, hfplan.BrowseOptions{AdultOK: true})
	require.Error(t, err)
	assert.Empty(t, l.Repo.ID)
	assert.Zero(t, srv.Count("/api/models/o/m/tree/"))
}

func TestPin_AnOlderCommitWhoseHeadCannotBeReadIsAnErrorEvenWithTheGateOpen(t *testing.T) {
	for name, set := range map[string]func(*hftest.Repo){
		"404":       func(r *hftest.Repo) { r.HeadStatus = http.StatusNotFound },
		"500":       func(r *hftest.Repo) { r.HeadStatus = http.StatusInternalServerError },
		"malformed": func(r *hftest.Repo) { r.HeadMalformed = true },
	} {
		t.Run(name, func(t *testing.T) {
			r := repo()
			r.OldCommit = oldCommit
			set(&r)
			srv := hftest.New(t, r)
			for _, adultOK := range []bool{false, true} {
				p, err := hfplan.Pin(context.Background(), client(srv, ""), "o/m", oldCommit, "pony-fp16.safetensors", hfplan.PinOptions{AdultOK: adultOK})
				require.Error(t, err, "adultOK=%v", adultOK)
				assert.NotErrorIs(t, err, hfplan.ErrAdult)
				assert.Equal(t, hfplan.Pinned{}, p, "no Pinned")
			}
			assert.Zero(t, srv.Count("/api/models/o/m/paths-info/"), "nothing of the file is read")
		})
	}
}

func TestPin_ARenamedRepositoryWhoseHeadCannotBeReadIsAnError(t *testing.T) {
	r := repo()
	r.OldCommit, r.HeadStatus = oldCommit, http.StatusInternalServerError
	srv := hftest.New(t, r)
	srv.Rename("o/old", "o/m")
	p, err := hfplan.Pin(context.Background(), client(srv, ""), "o/old", oldCommit, "pony-fp16.safetensors", hfplan.PinOptions{AdultOK: true})
	require.Error(t, err)
	assert.Equal(t, hfplan.Pinned{}, p)
	assert.Zero(t, srv.Count("/api/models/o/m/paths-info/"))
}
