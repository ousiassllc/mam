package runtimechoices_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/choice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/store"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime/lrtest"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmtest"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices"
	"github.com/ousiassllc/mam/backend/internal/service/runtimechoices/choicetest"
)

func TestMain(m *testing.M) {
	if lrtest.ServeIfAsked() {
		return
	}
	mmtest.Main(m)
}

func newService(t *testing.T) (runtimechoices.Service, *lrtest.Rig) {
	t.Helper()
	mm := mmtest.New(t)
	rig := lrtest.New(t, mm.Queue, mm.Table, mm.Svc.HandleInstall, choicetest.WithChoices())
	return runtimechoices.Service{Runtimes: rig.Svc}, rig
}

func viewOf(t *testing.T, s runtimechoices.Service, id string) runtimechoices.View {
	t.Helper()
	views, err := s.Views(context.Background(), lrtest.ID)
	require.NoError(t, err)
	for _, v := range views {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("no model %q in %v", id, views)
	return runtimechoices.View{}
}

func TestViews_TheDefaultIsInForceAndALicenseToAcceptIsMarked(t *testing.T) {
	s, _ := newService(t)

	small, large := viewOf(t, s, choicetest.SmallChoice), viewOf(t, s, choicetest.LargeChoice)

	assert.True(t, small.Active)
	assert.False(t, small.ConsentNeeded)
	assert.False(t, large.Active)
	assert.True(t, large.ConsentNeeded)
	assert.False(t, large.Accepted)
	assert.Equal(t, "不明なライセンス", large.LicenseInfo.Name)
	assert.Positive(t, small.SizeBytes)
	assert.NotEqual(t, small.SizeBytes, large.SizeBytes, "each model's size is its own weights and the shared ones")
	assert.NotEmpty(t, small.VRAMFit, "a model with a recommendation says how this GPU stands to it")
	assert.Empty(t, large.VRAMFit, "a model with no recommendation is not judged")
}

func TestViews_ARuntimeWithoutChoicesHasNoModelsAndAnUnknownOneIsNotFound(t *testing.T) {
	mm := mmtest.New(t)
	rig := lrtest.New(t, mm.Queue, mm.Table, mm.Svc.HandleInstall)
	s := runtimechoices.Service{Runtimes: rig.Svc}

	views, err := s.Views(context.Background(), lrtest.ID)
	require.NoError(t, err)
	assert.Empty(t, views)

	_, err = s.Views(context.Background(), "no-such-runtime")
	assert.Error(t, err)
}

func TestSelect_ARuntimeWhoseLicenseWasNotAcceptedStaysOnItsModel(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()

	for name, accept := range map[string]*runtimechoices.Accept{
		"none":          nil,
		"another text":  {Version: "terms-0"},
		"an empty text": {Version: ""},
	} {
		t.Run(name, func(t *testing.T) {
			err := s.Select(ctx, lrtest.ID, choicetest.LargeChoice, accept)

			assert.ErrorIs(t, err, choice.ErrConsentRequired)
			assert.True(t, viewOf(t, s, choicetest.SmallChoice).Active, "nothing was chosen")
			assert.False(t, viewOf(t, s, choicetest.LargeChoice).Accepted, "nothing was recorded")
		})
	}
}

func TestSelect_TheVersionOfTheTextTheUserWasShownIsRecordedAndNeverAskedAgain(t *testing.T) {
	s, rig := newService(t)
	ctx := context.Background()

	require.NoError(t, s.Select(ctx, lrtest.ID, choicetest.LargeChoice, &runtimechoices.Accept{Version: choicetest.LargeConsentVersion}))

	large := viewOf(t, s, choicetest.LargeChoice)
	assert.True(t, large.Active)
	assert.True(t, large.Accepted)
	assert.False(t, large.ConsentNeeded)
	st, err := rig.Svc.State(lrtest.ID)
	require.NoError(t, err)
	assert.Equal(t, choicetest.LargeChoice, st.Choice)
	assert.Equal(t, choicetest.LargeConsentVersion, st.Consents[choicetest.LargeLicense].Version)
	assert.False(t, st.Consents[choicetest.LargeLicense].AcceptedAt.IsZero())

	require.NoError(t, s.Select(ctx, lrtest.ID, choicetest.SmallChoice, nil), "a model with no license to accept needs none")
	require.NoError(t, s.Select(ctx, lrtest.ID, choicetest.LargeChoice, nil), "the acceptance of this version is kept")
	assert.True(t, viewOf(t, s, choicetest.LargeChoice).Active)
}

func TestSelect_ANewVersionOfTheTextAsksAgain(t *testing.T) {
	s, rig := newService(t)
	ctx := context.Background()
	require.NoError(t, s.Select(ctx, lrtest.ID, choicetest.LargeChoice, &runtimechoices.Accept{Version: choicetest.LargeConsentVersion}))
	require.NoError(t, rig.Svc.Update(lrtest.ID, func(r *store.RuntimeState) {
		r.Consents[choicetest.LargeLicense] = store.Consent{Version: "terms-0", AcceptedAt: time.Now()} // the text the user saw before it changed
	}))

	err := s.Select(ctx, lrtest.ID, choicetest.SmallChoice, nil)
	require.NoError(t, err)
	err = s.Select(ctx, lrtest.ID, choicetest.LargeChoice, nil)

	assert.ErrorIs(t, err, choice.ErrConsentRequired)
}

func TestSelect_AnUnknownModelIsRefused(t *testing.T) {
	s, _ := newService(t)

	err := s.Select(context.Background(), lrtest.ID, "no-such-model", nil)

	assert.ErrorIs(t, err, choice.ErrUnknown)
}

func TestSelect_TheModelInForceDecidesTheWeightsTheRuntimeNeeds(t *testing.T) {
	s, rig := newService(t)
	ctx := context.Background()
	before, err := rig.Svc.Status(ctx, lrtest.ID)
	require.NoError(t, err)

	require.NoError(t, s.Select(ctx, lrtest.ID, choicetest.LargeChoice, &runtimechoices.Accept{Version: choicetest.LargeConsentVersion}))
	after, err := rig.Svc.Status(ctx, lrtest.ID)
	require.NoError(t, err)

	assert.NotEqual(t, before.Weights.TotalBytes, after.Weights.TotalBytes)
	if assert.NotNil(t, after.Choice) {
		assert.Equal(t, choicetest.LargeChoice, after.Choice.ID)
	}
}
