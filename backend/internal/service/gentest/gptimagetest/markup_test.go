package gptimagetest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/chatgptid"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
)

// The markup image is laid over the first input image: it is not an input of the edit, it is the `Markup` of the call.
func TestAMarkupBecomesTheMarkupOfTheCallAndTheEditTargetStaysFirst(t *testing.T) {
	r := gentesttest.New(t)
	base1, _, err := r.Svc.StageReference(gentesttest.SolidPNG(4, 3, 255))
	require.NoError(t, err)
	markup, _, err := r.Svc.StageReference(gentesttest.SolidPNG(4, 3, 0))
	require.NoError(t, err)
	base2, _, err := r.Svc.StageReference(gentesttest.SolidPNG(2, 2, 255))
	require.NoError(t, err)
	req := withGPT(imageReq(flare, sunburst), low)
	req.References = []gentest.RefInput{
		{Source: gentest.FromStaged, ID: markup, Role: gentest.RoleMarkup}, // given first: the base is the first of the others all the same
		{Source: gentest.FromStaged, ID: base1, Role: gentest.RoleReference},
		{Source: gentest.FromStaged, ID: base2, Role: gentest.RoleReference},
	}
	st := runOK(t, r, req)
	for _, m := range []domain.ModelName{domain.ModelGPTImage26Flare, domain.ModelGPTImage26Sunburst} {
		call := r.Fake.CallsOf(m)
		require.Len(t, call, 1, m)
		img := call[0].Image
		require.NotNil(t, img.Markup, m)
		assert.Equal(t, gentesttest.SolidPNG(4, 3, 0), img.Markup.Data, m)
		require.Len(t, img.Inputs, 2, "%s: the markup is not an input", m)
		assert.Equal(t, gentesttest.SolidPNG(4, 3, 255), img.Inputs[0].Data, "%s: the edit target is first", m)
		assert.Len(t, img.Options.Refs, 2)
	}
	d, err := r.Svc.Result(context.Background(), st.Results[0].ID)
	require.NoError(t, err)
	assert.Empty(t, d.UnusedInputs, "the markup is used by Flare")
	var roles []string
	for _, in := range d.Inputs {
		roles = append(roles, in.Role)
	}
	assert.Contains(t, roles, "markup")
}

func markupReq(t *testing.T, r *gentesttest.Rig, model string, base, markup []byte) gentest.Request {
	t.Helper()
	b, _, err := r.Svc.StageReference(base)
	require.NoError(t, err)
	m, _, err := r.Svc.StageReference(markup)
	require.NoError(t, err)
	req := imageReq(model)
	req.References = []gentest.RefInput{
		{Source: gentest.FromStaged, ID: b, Role: gentest.RoleReference},
		{Source: gentest.FromStaged, ID: m, Role: gentest.RoleMarkup},
	}
	return req
}

func TestAMarkupIsRefusedWhenItCannotWork(t *testing.T) {
	r := gentesttest.New(t, gentesttest.WithSubscriptionImageModels())
	opaque, transparent, small := gentesttest.SolidPNG(4, 3, 255), gentesttest.SolidPNG(4, 3, 0), gentesttest.SolidPNG(2, 2, 0)

	t.Run("without an image to edit", func(t *testing.T) {
		m, _, err := r.Svc.StageReference(transparent)
		require.NoError(t, err)
		req := imageReq(flare)
		req.References = []gentest.RefInput{{Source: gentest.FromStaged, ID: m, Role: gentest.RoleMarkup}}
		refused(t, r, req, "マークアップ", "入力画像")
	})
	t.Run("the size differs", func(t *testing.T) {
		refused(t, r, markupReq(t, r, flare, opaque, small), "大きさ", "4 × 3", "2 × 2")
	})
	t.Run("nothing is transparent", func(t *testing.T) {
		refused(t, r, markupReq(t, r, flare, opaque, opaque), "透明な部分がありません")
	})
	t.Run("not a PNG", func(t *testing.T) {
		refused(t, r, markupReq(t, r, flare, opaque, []byte("\xff\xd8\xff\xe0not-a-png-at-all-but-a-jpeg-header")), "PNG")
	})
	t.Run("a model that cannot take it", func(t *testing.T) {
		for _, model := range []string{string(domain.ModelGPTImage2), string(domain.ModelNanoBanana)} {
			refused(t, r, markupReq(t, r, model, opaque, transparent), "マークアップに対応していません")
		}
	})
	t.Run("one of the chosen models cannot", func(t *testing.T) {
		req := markupReq(t, r, flare, opaque, transparent)
		req.Models = []string{flare, string(domain.ModelGPTImage2)}
		refused(t, r, req, "GPT Image 2")
	})
	t.Run("the subscription twin", func(t *testing.T) {
		refused(t, r, markupReq(t, r, string(chatgptid.Flare), opaque, transparent), "サブスク")
	})
	t.Run("two markups", func(t *testing.T) {
		req := markupReq(t, r, flare, opaque, transparent)
		m2, _, err := r.Svc.StageReference(transparent)
		require.NoError(t, err)
		req.References = append(req.References, gentest.RefInput{Source: gentest.FromStaged, ID: m2, Role: gentest.RoleMarkup})
		refused(t, r, req, "1 枚だけ")
	})
	t.Run("an SVG edit", func(t *testing.T) {
		refused(t, r, withGPT(markupReq(t, r, flare, opaque, transparent), gptimage.Options{Format: gptimage.FormatSVG}), "SVG")
	})
	t.Run("with an inpaint mask", func(t *testing.T) {
		req := markupReq(t, r, flare, opaque, transparent)
		w, _, err := r.Svc.StageReference(gentesttest.WhitePNG)
		require.NoError(t, err)
		req.Mask = &gentest.RefInput{Source: gentest.FromStaged, ID: w}
		refused(t, r, req, "マスク")
	})
	t.Run("on a video", func(t *testing.T) {
		req := gentest.Request{Media: "video", Models: []string{"kling-3.0"}, Prompts: []gentest.PromptInput{{Prompt: "x"}},
			References: []gentest.RefInput{{Source: gentest.FromStaged, ID: "x", Role: gentest.RoleMarkup}}}
		refused(t, r, req, "画像だけ")
	})
	assert.Empty(t, r.Fake.Calls(), "no call is made for a request that is refused")
}

func TestAMarkupThatFitsIsEstimatedAndRun(t *testing.T) {
	r := gentesttest.New(t)
	req := markupReq(t, r, flare, gentesttest.SolidPNG(4, 3, 255), gentesttest.SolidPNG(4, 3, 0))
	_, err := r.Svc.Estimate(context.Background(), req)
	require.NoError(t, err)
}

func TestMarkupIsAnInputRoleOfFlareAndSunburstOnly(t *testing.T) {
	r := gentesttest.New(t)
	ms := r.Svc.Models(context.Background(), "image")
	by := map[string]struct {
		roles []string
		caps  bool
	}{}
	for _, m := range ms {
		by[m.ID] = struct {
			roles []string
			caps  bool
		}{m.ReferenceRoles, m.GPTImage != nil}
	}
	for _, id := range []string{flare, sunburst} {
		assert.Contains(t, by[id].roles, gentest.RoleMarkup, id)
		assert.Contains(t, by[id].roles, gentest.RoleReference, id)
		assert.True(t, by[id].caps, id)
	}
	for _, id := range []string{string(domain.ModelGPTImage2), string(domain.ModelNanoBanana), string(domain.ModelFlux2)} {
		assert.NotContains(t, by[id].roles, gentest.RoleMarkup, id)
		assert.False(t, by[id].caps, id)
	}
}

// The Project's images are PNG, JPEG and WebP: a GIF or an SVG result is not imported, with the reason, and nothing is written.
func TestAGIFOrSVGResultIsNotImportedIntoAProject(t *testing.T) {
	r := gentesttest.New(t)
	project := r.NewProject()
	r.Fake.ReturnImage(domain.ModelGPTImage26Flare, modeladapter.ImageResult{Data: []byte("GIF89a-data"), MimeType: "image/gif"})
	st := runOK(t, r, withGPT(imageReq(flare), gptimage.Options{Format: gptimage.FormatGIF}))

	_, err := r.Svc.Import(context.Background(), projectlink.ImportRequest{ResultID: st.Results[0].ID, ProjectID: project, As: projectlink.AsAsset})
	var inv *gterr.InvalidError
	require.ErrorAs(t, err, &inv)
	assert.Contains(t, inv.Message, "GIF・SVG")
	assert.Zero(t, countRows(t, r, "asset"))
}
