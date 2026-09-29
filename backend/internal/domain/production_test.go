package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestProductionTemplate_AxesFollowThePriorityOfTheSpec(t *testing.T) {
	assert.Equal(t, domain.AxisProductAccuracy, domain.TemplateProductCM.Axes()[0], "PRODUCT/CM puts Product Accuracy first")
	assert.Equal(t, domain.AxisStory, domain.TemplateDrama.Axes()[0])
	assert.Equal(t, domain.AxisActing, domain.TemplateDrama.Axes()[1])
	assert.Equal(t, domain.AxisCharacterDesign, domain.TemplateAnime.Axes()[0])
	assert.Equal(t, domain.AxisOpeningHook, domain.TemplateSNSShort.Axes()[0])
	assert.Len(t, domain.TemplateProductCM.Axes(), 8)
	assert.Len(t, domain.TemplateAnime.Axes(), 7)

	for _, tpl := range domain.ProductionTemplates() {
		assert.True(t, tpl.Valid())
		for _, a := range tpl.Axes() {
			assert.NotEqual(t, string(a), a.Label(), "axis %s has a label", a)
		}
	}
	assert.False(t, domain.ProductionTemplate("movie").Valid())
}

func TestProductionTemplate_WeightsFavourTheHigherPriorityAndSumToOne(t *testing.T) {
	for _, tpl := range domain.ProductionTemplates() {
		var sum float64
		axes := tpl.Axes()
		w := tpl.Weights()
		for i, a := range axes {
			sum += w[a]
			if i > 0 {
				assert.Greater(t, w[axes[i-1]], w[a], "%s: %s outweighs %s", tpl, axes[i-1], a)
			}
		}
		assert.InDelta(t, 1, sum, 1e-9, tpl)
	}
}

func TestProductionTemplate_SwitchingTheTemplateChangesWhichWeakAxisHurtsMost(t *testing.T) {
	// The same evaluation: strong Camera, weak Product Accuracy / Story.
	scores := map[domain.EvaluationAxis]float64{
		domain.AxisProductAccuracy: 40, domain.AxisStory: 40, domain.AxisCamera: 100, domain.AxisVisualQuality: 100,
	}
	cm, ok := domain.TemplateProductCM.WeightedScore(scores)
	require.True(t, ok)
	drama, ok := domain.TemplateDrama.WeightedScore(scores)
	require.True(t, ok)

	// Product Accuracy is the top axis of PRODUCT/CM and Story of DRAMA; Camera is 7th of DRAMA.
	assert.InDelta(t, (40*8+100*1)/9.0, cm, 1e-9, "PRODUCT/CM: Product Accuracy (weight 8) and Visual Quality (weight 1)")
	assert.InDelta(t, (40*8+100*2)/10.0, drama, 1e-9, "DRAMA: Story (weight 8) and Camera (weight 2)")
}

func TestProductionTemplate_WeightedScoreIgnoresForeignAxesAndReportsNoScore(t *testing.T) {
	got, ok := domain.TemplateDrama.WeightedScore(map[domain.EvaluationAxis]float64{domain.AxisStory: 80, domain.AxisHook: 10})
	require.True(t, ok)
	assert.InDelta(t, 80, got, 1e-9, "Hook is not a DRAMA axis")

	_, ok = domain.TemplateDrama.WeightedScore(map[domain.EvaluationAxis]float64{domain.AxisHook: 10})
	assert.False(t, ok)
	_, ok = domain.TemplateDrama.WeightedScore(nil)
	assert.False(t, ok)
}
