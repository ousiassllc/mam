package inpainttest

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

func TestInpaintRefusesWhatTheModelCannotDo(t *testing.T) {
	rig := gentesttest.New(t, gentesttest.WithInpaintModel())
	src := stage(t, rig, gentesttest.PNG)
	white := stage(t, rig, gentesttest.WhitePNG)
	black := stage(t, rig, gentesttest.BlackPNG)
	ctx := t.Context()

	base := func(model, mask string, denoise *float64) gentest.Request {
		return gentest.Request{
			Media: gentest.MediaImage, Models: []string{model}, Count: 1,
			Prompts:    []gentest.PromptInput{{Prompt: "a fox"}},
			References: []gentest.RefInput{{Source: gentest.FromStaged, ID: src, Role: gentest.RoleReference}},
			Mask:       &gentest.RefInput{Source: gentest.FromStaged, ID: mask}, Denoise: denoise,
		}
	}
	_, err := rig.Svc.Estimate(ctx, base(string(gentesttest.PlainModel), white, f64(0.5)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "対応していない")

	_, err = rig.Svc.Estimate(ctx, base(string(gentesttest.InpaintModel), black, f64(0.5)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "真っ黒")

	_, err = rig.Svc.Estimate(ctx, base(string(gentesttest.InpaintModel), white, f64(0)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "0 より大きく")

	noMask := base(string(gentesttest.InpaintModel), white, f64(0.5))
	noMask.Mask = nil
	_, err = rig.Svc.Estimate(ctx, noMask)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "マスク")

	video := base(string(gentesttest.InpaintModel), white, f64(0.5))
	video.Media = gentest.MediaVideo
	_, err = rig.Svc.Estimate(ctx, video)
	require.Error(t, err)
}

// hugePNGHeader is a PNG that is only a signature and a header of 100000 × 100000: DecodeConfig reads it, a full decode would need 10 GB.
func hugePNGHeader() []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 100000)
	binary.BigEndian.PutUint32(ihdr[4:], 100000)
	ihdr[8], ihdr[9] = 8, 0 // 8 bit, gray
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestInpaintRefusesAMaskOfHugeSizeWithoutDecodingIt(t *testing.T) {
	rig := gentesttest.New(t, gentesttest.WithInpaintModel())
	src := stage(t, rig, gentesttest.PNG)
	huge := stage(t, rig, hugePNGHeader())
	_, err := rig.Svc.Estimate(t.Context(), gentest.Request{
		Media: gentest.MediaImage, Models: []string{string(gentesttest.InpaintModel)}, Count: 1,
		Prompts:    []gentest.PromptInput{{Prompt: "a fox"}},
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: src, Role: gentest.RoleReference}},
		Mask:       &gentest.RefInput{Source: gentest.FromStaged, ID: huge}, Denoise: f64(0.5),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "大きすぎ")
}

func TestInpaintOnlyModelNeedsAMask(t *testing.T) {
	rig := gentesttest.New(t, gentesttest.WithInpaintModel())
	src := stage(t, rig, gentesttest.PNG)
	_, err := rig.Svc.Estimate(t.Context(), gentest.Request{
		Media: gentest.MediaImage, Models: []string{string(gentesttest.InpaintModel)}, Count: 1,
		Prompts:    []gentest.PromptInput{{Prompt: "a fox"}},
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: src, Role: gentest.RoleReference}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mask を指定してください")
}

func TestInpaintRunSendsTheMaskAndTheDenoise(t *testing.T) {
	rig := gentesttest.New(t, gentesttest.WithInpaintModel())
	src := stage(t, rig, gentesttest.PNG)
	white := stage(t, rig, gentesttest.WhitePNG)
	ctx := t.Context()
	req := gentest.Request{
		Media: gentest.MediaImage, Models: []string{string(gentesttest.InpaintModel)}, Count: 1,
		Prompts:    []gentest.PromptInput{{Prompt: "a fox in the white part"}},
		References: []gentest.RefInput{{Source: gentest.FromStaged, ID: src, Role: gentest.RoleReference}},
		Mask:       &gentest.RefInput{Source: gentest.FromStaged, ID: white}, Denoise: f64(0.4),
	}
	est, err := rig.Svc.Estimate(ctx, req)
	require.NoError(t, err)
	ref, err := rig.Svc.Run(ctx, req, gentest.Confirmation{EstimateID: est.ID, TotalUSD: est.TotalUSD})
	require.NoError(t, err)
	st := rig.Wait(ref.BatchID)
	require.Equal(t, "succeeded", st.State)
	calls := rig.Fake.CallsOf(gentesttest.InpaintModel)
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].Image.Inpaint)
	assert.NotEmpty(t, calls[0].Image.Inpaint.Mask.Data)
	require.NotNil(t, calls[0].Image.Inpaint.Denoise)
	assert.InDelta(t, 0.4, *calls[0].Image.Inpaint.Denoise, 1e-9)
	assert.Len(t, calls[0].Image.Inputs, 1, "the mask is not a reference")

	d, err := rig.Svc.Result(ctx, st.Results[0].ID)
	require.NoError(t, err)
	require.NotNil(t, d.Inpaint)
	require.NotNil(t, d.Inpaint.Denoise)
	assert.InDelta(t, 0.4, *d.Inpaint.Denoise, 1e-9)
	assert.Equal(t, gentest.FromStaged, d.Inpaint.SourceKind)
	assert.NotEmpty(t, d.Inpaint.MaskInput)
}

func stage(t *testing.T, rig *gentesttest.Rig, data []byte) string {
	t.Helper()
	id, _, err := rig.Svc.StageReference(data)
	require.NoError(t, err)
	return id
}

func f64(v float64) *float64 { return &v }
