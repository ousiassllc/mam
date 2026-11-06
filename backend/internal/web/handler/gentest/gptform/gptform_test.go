package gptform_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/web/handler/gentest/gptform"
)

func TestTheFieldsOfTheFormBecomeTheOptionsOfTheRequest(t *testing.T) {
	f := gptform.Read(url.Values{
		"gpt_complexity": {" low "}, "gpt_format": {"gif"}, "gpt_frame_count": {"12"}, "gpt_frame_duration_ms": {"80"},
	})
	var r gentest.Request
	require.NoError(t, f.Apply(&r))
	assert.Equal(t, gptimage.Options{Complexity: "low", Format: "gif", FrameCount: 12, FrameDurationMS: 80}, r.GPTImage)

	var none gentest.Request
	require.NoError(t, gptform.Read(url.Values{}).Apply(&none))
	assert.True(t, none.GPTImage.Zero(), "a form without the section sends none of them")
}

func TestANumberThatIsNotOneIsRefusedNotDropped(t *testing.T) {
	for _, v := range []url.Values{{"gpt_frame_count": {"abc"}}, {"gpt_frame_duration_ms": {"1.5"}}} {
		var r gentest.Request
		err := gptform.Read(v).Apply(&r)
		var inv *gterr.InvalidError
		require.ErrorAs(t, err, &inv)
	}
}

func TestHiddenCarriesExactlyWhatWasGiven(t *testing.T) {
	assert.Empty(t, gptform.Hidden(gentest.Request{}))
	got := gptform.Hidden(gentest.Request{GPTImage: gptimage.Options{Complexity: "high", Format: "svg"}})
	assert.Len(t, got, 2)
	assert.Equal(t, "gpt_complexity", got[0].Name)
	assert.Equal(t, "svg", got[1].Value)
}

func TestTheLinesOfAResultSayWhatItWasAskedFor(t *testing.T) {
	assert.Empty(t, gptform.Lines(record.ResultView{}), "a result that used none says nothing")
	gif := gptform.Lines(record.ResultView{GPT: &gptimage.Options{Format: "gif", FrameCount: 12}})
	assert.Equal(t, []gptform.Line{
		{Key: "複雑さ", Value: "指定なし（OpenAI の既定: high）"}, {Key: "出力形式", Value: "GIF"},
		{Key: "フレーム数", Value: "12"}, {Key: "フレームの長さ", Value: "指定なし（OpenAI の既定: 200 ms）"},
	}, gif)
	svg := gptform.Lines(record.ResultView{GPT: &gptimage.Options{Complexity: "low", Format: "svg"}})
	assert.Equal(t, "SVG（費用は見積れません）", svg[1].Value)
	assert.Len(t, svg, 2, "an SVG has no frames")
}

func TestTheSectionFollowsTheTickedModels(t *testing.T) {
	flare := modelcatalog.Info{ID: "flare", Label: "Flare", Available: true, GPTImage: gptimage.CapsOf("gpt-image-2.6-flare")}
	sub := modelcatalog.Info{ID: "sub", Label: "Flare（ChatGPT）", Available: true, GPTImageRefusal: gptimage.SubscriptionRefusal}
	plain := modelcatalog.Info{ID: "plain", Label: "SDXL", Available: true}
	all := []modelcatalog.Info{plain, flare, sub}

	d := gptform.SectionOf(gptform.Fields{}, all, nil)
	assert.True(t, d.Offered)
	assert.False(t, d.Shown, "nothing ticked takes them")

	d = gptform.SectionOf(gptform.Fields{}, all, []string{"flare"})
	assert.True(t, d.Shown && d.Fields)
	assert.Equal(t, gptimage.MaxFrameCount, d.MaxFrames)

	d = gptform.SectionOf(gptform.Fields{}, all, []string{"sub"})
	assert.True(t, d.Shown)
	assert.False(t, d.Fields, "nothing is posted for a model that is refused")
	assert.Equal(t, []string{"Flare（ChatGPT）: " + gptimage.SubscriptionRefusal}, d.Notes)

	d = gptform.SectionOf(gptform.Fields{Complexity: "low"}, all, []string{"plain"})
	assert.True(t, d.Shown && d.Fields, "a value that is given stays in the form")
	assert.Equal(t, []string{gptform.OtherNote("SDXL")}, d.Notes)

	assert.False(t, gptform.SectionOf(gptform.Fields{}, []modelcatalog.Info{plain}, []string{"plain"}).Offered, "no model of the family: no section")
}
