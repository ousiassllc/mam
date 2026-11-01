package locallipsync_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
	rtm "github.com/ousiassllc/mam/backend/internal/modelmgr/runtime"
	"github.com/ousiassllc/mam/backend/internal/service/locallipsync"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

// runtime is the runtime manager: Prepare and LipSync answer with the errors the test sets.
type runtime struct {
	prepareErr, lipSyncErr error
	prepared               []string
	got                    lipsync.Request
}

func (r *runtime) Prepare(_ context.Context, id string) error {
	r.prepared = append(r.prepared, id)
	return r.prepareErr
}

func (r *runtime) LipSync(_ context.Context, req lipsync.Request) (lipsync.Response, error) {
	r.got = req
	return lipsync.Response{OutputPath: req.OutputPath, Frames: 30}, r.lipSyncErr
}

func request() lipsync.Request {
	abs := func(n string) string { return filepath.Join(os.TempDir(), n) }
	return lipsync.Request{VideoPath: abs("in.mp4"), AudioPath: abs("in.wav"), OutputPath: abs("out.mp4"), FFmpegPath: abs("ffmpeg")}
}

func TestPrepare_StartsTheMuseTalkRuntimeOfTheCatalog(t *testing.T) {
	rt := &runtime{}

	require.NoError(t, locallipsync.Runner{Runtime: rt}.Prepare(context.Background()))

	assert.Equal(t, []string{"musetalk"}, rt.prepared)
}

// Every refusal of the manager has its Kind, and the error is the lip sync's (not the voice's).
func TestPrepare_EveryRefusalOfTheManagerIsAnUnavailableErrorOfTheLipSync(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		kind localvoice.Kind
	}{
		"not installed": {localruntime.ErrNotInstalled, localvoice.KindNotInstalled},
		"weights":       {&localruntime.WeightsError{Runtime: "musetalk", Name: "MuseTalk 1.5", Assets: []rtm.AssetStatus{{Name: "unet.pth", State: rtm.AssetMissing}}}, localvoice.KindWeightsMissing},
		"macOS":         {&localruntime.SupportError{Runtime: "musetalk", Support: rtm.Support{Code: rtm.CodeOS, Reason: "macOS では動きません", Fallback: "Sync Labs を使ってください"}}, localvoice.KindUnsupported},
		"low VRAM":      {&localruntime.SupportError{Runtime: "musetalk", Support: rtm.Support{Code: rtm.CodeLowVRAM, Reason: "VRAM が足りません"}}, localvoice.KindGPUMemory},
		"start failed":  {errors.New("MuseTalk を起動できません"), localvoice.KindProcess},
		"start timeout": {fmt.Errorf("MuseTalk が起動しませんでした: %w", context.DeadlineExceeded), localvoice.KindTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			err := locallipsync.Runner{Runtime: &runtime{prepareErr: tc.err}}.Prepare(context.Background())

			var un *localvoice.UnavailableError
			require.ErrorAs(t, err, &un)
			assert.Equal(t, tc.kind, un.Kind)
			assert.Equal(t, localvoice.VendorMuseTalk, un.Vendor)
			assert.ErrorIs(t, err, tc.err)
		})
	}
}

func TestPrepare_TheCallersCancelStaysAsItIs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := locallipsync.Runner{Runtime: &runtime{prepareErr: context.Canceled}}.Prepare(ctx)

	require.ErrorIs(t, err, context.Canceled)
	var un *localvoice.UnavailableError
	assert.False(t, errors.As(err, &un))
}

func TestLipSync_AnswersOfTheServerPassAsTheyAre(t *testing.T) {
	rt := &runtime{}
	res, err := locallipsync.Runner{Runtime: rt}.LipSync(context.Background(), request())
	require.NoError(t, err)
	assert.Equal(t, 30, res.Frames)
	assert.Equal(t, request(), rt.got)

	for name, in := range map[string]error{
		"the server refused": &lipsync.Error{Code: lipsync.CodeNoFace, Status: 422, Message: "顔が無い"},
		"does not answer":    fmt.Errorf("server: %w", lipsync.ErrUnreachable),
		"a bad request":      fmt.Errorf("%w: video_path", lipsync.ErrBadRequest),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := locallipsync.Runner{Runtime: &runtime{lipSyncErr: in}}.LipSync(context.Background(), request())
			assert.Equal(t, in, err, "the client reads it")
		})
	}
}

// What the manager refuses before the generation (not installed, weights, not supported, start failed) is the typed error.
func TestLipSync_ARefusalBeforeTheGenerationIsAnUnavailableError(t *testing.T) {
	_, err := locallipsync.Runner{Runtime: &runtime{lipSyncErr: localruntime.ErrNotInstalled}}.LipSync(context.Background(), request())

	var un *localvoice.UnavailableError
	require.ErrorAs(t, err, &un)
	assert.Equal(t, localvoice.KindNotInstalled, un.Kind)
	assert.Equal(t, localvoice.VendorMuseTalk, un.Vendor)
}
