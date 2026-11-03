package inputcheck_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/inputcheck"
)

var (
	t2v    = modeladapter.VideoCapabilities{}
	i2v    = modeladapter.VideoCapabilities{FirstFrame: true}
	flf    = modeladapter.VideoCapabilities{FirstFrame: true, LastFrame: true}
	refs2  = modeladapter.VideoCapabilities{MaxReferenceImages: 2}
	frames = []string{"first_frame", "last_frame"}
)

// The boundary of the check: all of the images unused is a stop (AllUnused), only some of them is a plan with notes and unused inputs.
func TestVideoPlans(t *testing.T) {
	cases := []struct {
		name   string
		caps   modeladapter.VideoCapabilities
		roles  []string
		used   []int
		unused []int
		notes  int
	}{
		{"text to video takes none", t2v, []string{"first_frame"}, nil, []int{0}, 1},
		{"no image, nothing to say", i2v, nil, nil, nil, 0},
		{"first frame used", i2v, []string{"first_frame"}, []int{0}, nil, 0},
		{"a reference is not a first frame", i2v, []string{"reference"}, nil, []int{0}, 1},
		{"last frame without support is only warned", i2v, frames, []int{0}, []int{1}, 1},
		{"both frames used", flf, frames, []int{0, 1}, nil, 0},
		{"a last frame needs the first", flf, []string{"last_frame"}, nil, []int{0}, 1},
		{"too many references", refs2, []string{"reference", "reference", "reference"}, []int{0, 1}, []int{2}, 1},
		{"frames win over references", flf, []string{"first_frame", "reference"}, []int{0}, []int{1}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := inputcheck.Video(c.caps, "M", c.roles)
			assert.Equal(t, c.used, p.Used)
			var unused []int
			for _, u := range p.Unused {
				unused = append(unused, u.Index)
				assert.NotEmpty(t, u.Reason)
			}
			assert.Equal(t, c.unused, unused)
			assert.Len(t, p.Notes, c.notes)
		})
	}
}

func TestMissingNamesWhatToChange(t *testing.T) {
	m := inputcheck.Model{ID: "i2v", Label: "Wan I2V", Video: true, Requires: []string{"first_frame"}}
	assert.Nil(t, inputcheck.Missing(true, []string{"first_frame"}, m))

	e := inputcheck.Missing(true, []string{"reference"}, m)
	assert.Equal(t, apperr.CodeInputMissing, e.Code)
	assert.Contains(t, e.View(apperr.LangJa).Cause, "「Wan I2V」は最初のフレームの画像が必要です。添えた画像が「参照」になっています。「最初のフレーム」に変えてください。")

	e = inputcheck.Missing(true, nil, m)
	assert.Contains(t, e.View(apperr.LangJa).Cause, "画像を添えて、役割を「最初のフレーム」にしてください")

	ref := inputcheck.Model{ID: "cn", Label: "CN", Requires: []string{inputcheck.RequiredReference}}
	assert.NotNil(t, inputcheck.Missing(false, nil, ref))
	assert.Nil(t, inputcheck.Missing(false, []string{"face"}, ref), "any picture of an image batch is a reference")
}

func TestAllUnusedStopsOnlyWhenNoModelUsesAnImage(t *testing.T) {
	text := inputcheck.Model{ID: "t", Label: "T2V"}
	pic := inputcheck.Model{ID: "i", Label: "I2V", Video: true, TakesImages: true}
	none, one := inputcheck.Plan{Unused: []inputcheck.Unused{{Reason: "x"}}}, inputcheck.Plan{Used: []int{0}}

	assert.Nil(t, inputcheck.AllUnused(nil, []inputcheck.Model{text}, []inputcheck.Plan{none}), "no image: nothing to stop")
	e := inputcheck.AllUnused([]string{"first_frame"}, []inputcheck.Model{text}, []inputcheck.Plan{none})
	assert.Equal(t, apperr.CodeInputUnused, e.Code)
	assert.Contains(t, e.View(apperr.LangJa).Cause, "文章だけから作るモデルなので、添えた画像は使われません")
	assert.Nil(t, inputcheck.AllUnused([]string{"first_frame"}, []inputcheck.Model{text, pic}, []inputcheck.Plan{none, one}), "one model uses it: a warning for the other")
	e = inputcheck.AllUnused([]string{"reference"}, []inputcheck.Model{pic, text}, []inputcheck.Plan{none, none})
	assert.Contains(t, e.View(apperr.LangJa).Cause, "選んだモデルのどれでも使われません")
}
