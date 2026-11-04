package joblist_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/jobrecent"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/poses/posestest"
	"github.com/ousiassllc/mam/backend/internal/service/joblist"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
)

// A batch that was given a pose of a suggestive series (sexy, duo; Issue #1020) is listed like any batch, and the list says nothing of the pose, neither
// with the gate open nor with it closed: the entry has the models, the counts and the state, never the pictures an input was taken from.
func TestRecent_ABatchWithAnAdultPoseNamesNeitherThePoseNorItsPicture(t *testing.T) {
	lib := posestest.New(t)
	lib.InstallAdult(t) // the age gate is open
	rig := gentesttest.New(t, gentesttest.WithReferenceModels(""), gentesttest.WithPoseLibrary(lib.Lib))
	svc := servicetest.New(t, service.Deps{DB: rig.DB, Credentials: credentialtest.New()})
	list := joblist.New(joblist.Deps{
		Table: jobrecent.New(rig.DB), Shots: repository.NewGenerationRepository(rig.DB), Projects: svc.Projects, Gentest: rig.Svc,
	})
	ref, err := rig.Confirmed(gentest.Request{
		Media: "image", Models: []string{string(gentesttest.EditModel)}, Prompts: []gentest.PromptInput{{Prompt: "a fox"}},
		References: []gentest.RefInput{{Source: gentest.FromPoseLibrary, ID: "sexy_S001_front", Role: gentest.RolePose}},
	})
	require.NoError(t, err)
	rig.Wait(ref.BatchID)

	for _, open := range []bool{true, false} {
		lib.Open.Store(open)
		entries, err := list.Recent(t.Context())
		require.NoError(t, err)
		require.Len(t, entries, 1)
		text := fmt.Sprintf("%+v", entries)
		for _, leak := range []string{"sexy_", "S001", "S字立ち", "Standing S-curve", "poses/", "pose_library"} {
			assert.NotContains(t, text, leak, "the gate open=%v", open)
		}
	}
}
