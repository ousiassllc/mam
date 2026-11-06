package servicetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// #928: every model the generation test lists says whether it may be used commercially, with the license, the source and the day it was
// read. A model that is added to the adapter without its entry in data/cloud.json (or whose entry is incomplete) fails here.
func TestEveryBuiltInModelHasItsCommercialUseLicense(t *testing.T) {
	r := gentesttest.New(t)

	models := r.Svc.Models(context.Background(), "")
	require.NotEmpty(t, models)
	for _, m := range models {
		assert.NoError(t, m.License.Check(), m.ID)
		got, ok := modellicense.Cloud(m.ID)
		require.True(t, ok, "%s has no entry in modellicense data/cloud.json", m.ID)
		assert.Equal(t, got, m.License, m.ID)
	}
}

// The entries of data/cloud.json are models of the adapter: a typo in an id would leave the real model without an answer (it would be
// "unknown") and the entry unused.
func TestEveryEntryOfTheCloudLicensesIsAModel(t *testing.T) {
	known := map[string]bool{}
	for _, e := range chatgptid.Entries() {
		known[string(e.Model)] = true
	}
	for _, m := range domain.ImageModels() {
		known[string(m)] = true
	}
	for _, m := range domain.VideoModels() {
		known[string(m)] = true
	}
	for _, id := range modellicense.CloudIDs() {
		assert.True(t, known[id], "%s is not a model of the adapter", id)
	}
}

func TestModelsForNarrowsByTheCommercialFilter(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()

	all, filter, err := r.Svc.ModelsFor(ctx, "video", "all")
	require.NoError(t, err)
	assert.Equal(t, modellicense.FilterAll, filter)
	require.NotEmpty(t, all)

	ok, _, err := r.Svc.ModelsFor(ctx, "video", "ok")
	require.NoError(t, err)
	require.NotEmpty(t, ok)
	assert.Less(t, len(ok), len(all), "a video model whose answer is not yes is left out")
	for _, m := range ok {
		assert.Equal(t, modellicense.Yes, m.License.Commercial, m.ID)
	}

	cond, _, err := r.Svc.ModelsFor(ctx, "video", "ok_or_conditional")
	require.NoError(t, err)
	assert.Greater(t, len(cond), len(ok), "luma-ray-3.2 is 条件つき")
	for _, m := range cond {
		assert.Contains(t, []modellicense.Commercial{modellicense.Yes, modellicense.Conditional}, m.License.Commercial, m.ID)
	}
}

func TestModelsForRefusesAFilterItDoesNotKnow(t *testing.T) {
	r := gentesttest.New(t)

	_, _, err := r.Svc.ModelsFor(context.Background(), "", "commercial-only")

	var invalid *gterr.InvalidError
	require.ErrorAs(t, err, &invalid)
	assert.Contains(t, invalid.Message, "ok_or_conditional")
}

func TestModelsForStartsAtTheSettingsDefaultWhenNoFilterIsGiven(t *testing.T) {
	filter := modellicense.FilterAll
	modellicense.SetDefaultFilter(func() modellicense.Filter { return filter })
	t.Cleanup(func() { modellicense.SetDefaultFilter(nil) })
	r := gentesttest.New(t)
	ctx := context.Background()

	everything, got, err := r.Svc.ModelsFor(ctx, "image", "")
	require.NoError(t, err)
	assert.Equal(t, modellicense.FilterAll, got)

	filter = modellicense.FilterOK
	onlyOK, got, err := r.Svc.ModelsFor(ctx, "image", "")
	require.NoError(t, err)
	assert.Equal(t, modellicense.FilterOK, got, "the setting 商用OKのモデルだけを使う is the default")
	assert.LessOrEqual(t, len(onlyOK), len(everything))
	for _, m := range onlyOK {
		assert.Equal(t, modellicense.Yes, m.License.Commercial)
	}

	explicit, got, err := r.Svc.ModelsFor(ctx, "image", "all")
	require.NoError(t, err)
	assert.Equal(t, modellicense.FilterAll, got, "an explicit filter overrides the setting")
	assert.Len(t, explicit, len(everything))
}
