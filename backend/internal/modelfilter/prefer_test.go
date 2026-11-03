package modelfilter_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelfilter"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog/traits"
)

func TestInputsOfRoles(t *testing.T) {
	assert.Equal(t, []traits.Input{traits.FirstFrame, traits.ReferenceWhole, traits.LastFrame}, modelfilter.InputsOfRoles([]string{"first_frame", "reference", "first_frame", "mask", "last_frame"}))
	assert.Empty(t, modelfilter.InputsOfRoles(nil))
	assert.Equal(t, []traits.Input{traits.ReferenceFace, traits.Edit, traits.ReferencePose}, modelfilter.InputsOfRoles([]string{"face", "edit", "pose"}))
}

// Models that take the attached inputs come first; the others stay, in their order (a stable sort that hides nothing).
func TestPreferForInputs(t *testing.T) {
	with := func(id string, in ...traits.Input) modelfilter.Item {
		return item(id, func(i *modelfilter.Item) { i.Inputs = in })
	}
	list := []modelfilter.Item{with("t2v-a", traits.T2V), with("i2v-a", traits.I2V, traits.FirstFrame), with("t2v-b", traits.T2V),
		with("flf", traits.I2V, traits.FirstFrame, traits.LastFrame), with("i2v-b", traits.I2V, traits.FirstFrame)}
	id := func(it modelfilter.Item) modelfilter.Item { return it }

	got := modelfilter.PreferForInputs(append([]modelfilter.Item(nil), list...), id, []traits.Input{traits.FirstFrame})
	assert.Equal(t, []string{"i2v-a", "flf", "i2v-b", "t2v-a", "t2v-b"}, ids(got))

	got = modelfilter.PreferForInputs(append([]modelfilter.Item(nil), list...), id, []traits.Input{traits.FirstFrame, traits.LastFrame})
	assert.Equal(t, []string{"flf", "i2v-a", "i2v-b", "t2v-a", "t2v-b"}, ids(got), "a model that takes both comes before one that takes one")

	got = modelfilter.PreferForInputs(append([]modelfilter.Item(nil), list...), id, nil)
	assert.Equal(t, ids(list), ids(got), "no input attached: the order is kept")
}
