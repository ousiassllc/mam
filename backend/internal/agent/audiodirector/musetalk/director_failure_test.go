package musetalk_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/localvoice"
	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector/musetalk"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/lipsync"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// What the Director does when the local lip sync fails: one clip, or the whole runtime.

// A failure of one clip (no face, out of video memory, over its time, an empty answer) leaves that clip out with the reason; the
// speech and the other clips are made.
func TestDirector_AFailureOfOneClipLeavesOnlyThatClipOut(t *testing.T) {
	for name, tc := range map[string]struct {
		answer   answerFunc
		contains string
	}{
		"no face": {
			fails(&lipsync.Error{Code: lipsync.CodeNoFace, Status: 422, Message: "動画から顔を見つけられませんでした。サーバーの文"}),
			"動画から顔を見つけられなかったので、口パクを作れませんでした（動画から顔を見つけられませんでした。サーバーの文）",
		},
		"out of video memory": {
			fails(&lipsync.Error{Code: lipsync.CodeOutOfMemory, Status: 503, Message: "ビデオメモリが足りません。サーバーの文"}),
			"ローカルの口パク（MuseTalk 1.5）に GPU のメモリが足りません（ビデオメモリが足りません。サーバーの文）",
		},
		"too large":          {fails(&lipsync.Error{Code: lipsync.CodeTooLarge, Status: 413, Message: "短く切ってください"}), "動画が大きすぎて"},
		"the server stopped": {fails(lipsync.ErrUnreachable), "が動きませんでした"},
		"an empty answer": {func(_ context.Context, _ int, req lipsync.Request) (lipsync.Response, error) {
			return lipsync.Response{OutputPath: req.OutputPath}, os.WriteFile(req.OutputPath, nil, 0o600)
		}, "口パクの動画が空でした"},
	} {
		t.Run(name, func(t *testing.T) {
			// the first clip fails, the others are made
			r := newRig(t, &fakeRuntime{answer: func(ctx context.Context, n int, req lipsync.Request) (lipsync.Response, error) {
				if n == 0 {
					return tc.answer(ctx, n, req)
				}
				return copiesTheClip(ctx, n, req)
			}})

			tl, err := r.produce(t, localvoice.LipSyncLocal)

			require.NoError(t, err)
			require.Len(t, r.rt.calls, 3, "the clips after the failed one are tried")
			require.Len(t, tl.LipSync, 2)
			assert.Equal(t, []int{2, 3}, []int{tl.LipSync[0].ClipIndex, tl.LipSync[1].ClipIndex})
			sk := skipsOfLipSync(tl)
			require.Len(t, sk, 1)
			assert.Equal(t, domain.SkipError, sk[0].Reason)
			assert.Equal(t, "一つ目", sk[0].Prompt, "the line of the clip that was left out")
			assert.Contains(t, sk[0].Detail, tc.contains)
			assert.Equal(t, []float64{0, 0}, r.cost.costs(domain.ModelMuseTalk), "a clip that was not made is not recorded")
			assert.NotEmpty(t, tl.MixPath, "the audio is made")
			assert.Equal(t, 3, spoken(tl), "the speech is kept")
		})
	}
}

func TestDirector_AClipOverItsTimeLimitIsLeftOutAndTheNextOneIsTried(t *testing.T) {
	r := newRig(t, &fakeRuntime{answer: func(ctx context.Context, n int, req lipsync.Request) (lipsync.Response, error) {
		if n == 0 {
			<-ctx.Done()
			return lipsync.Response{}, ctx.Err()
		}
		return copiesTheClip(ctx, n, req)
	}})
	r.d.Providers.LocalLipSync = musetalk.New(r.rt, musetalk.Config{FFmpegPath: ffmpegOf(t), WorkDir: t.TempDir(), BaseTimeout: 50 * time.Millisecond, PerSecondTimeout: time.Nanosecond, MaxTimeout: 50 * time.Millisecond})

	tl, err := r.produce(t, localvoice.LipSyncLocal)

	require.NoError(t, err)
	require.Len(t, tl.LipSync, 2)
	sk := skipsOfLipSync(tl)
	require.Len(t, sk, 1)
	assert.Contains(t, sk[0].Detail, "時間内に終わりませんでした")
}

func ffmpegOf(t *testing.T) string {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	return tools.FFmpegPath()
}

// A runtime that cannot be used (not installed, no weights, not supported here) is found out once, before the first clip: one entry in
// Skipped says so, no clip is tried, and the audio is made without Lip Sync.
func TestDirector_ARuntimeThatCannotBeUsedIsKnownOnceAndNoClipIsTried(t *testing.T) {
	for name, tc := range map[string]struct {
		err      *localvoice.UnavailableError
		contains string
	}{
		"not installed":   {&localvoice.UnavailableError{Kind: localvoice.KindNotInstalled, Vendor: localvoice.VendorMuseTalk}, "導入されていません"},
		"no weights":      {&localvoice.UnavailableError{Kind: localvoice.KindWeightsMissing, Vendor: localvoice.VendorMuseTalk, Detail: "unet.pth"}, "（unet.pth）"},
		"macOS":           {&localvoice.UnavailableError{Kind: localvoice.KindUnsupported, Vendor: localvoice.VendorMuseTalk, Detail: "macOS では動きません"}, "この PC ではローカルの口パク（MuseTalk 1.5）を使えません（macOS では動きません）"},
		"too little VRAM": {&localvoice.UnavailableError{Kind: localvoice.KindGPUMemory, Vendor: localvoice.VendorMuseTalk, Detail: "4 GB 必要"}, "GPU のメモリが足りません（4 GB 必要）"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, &fakeRuntime{prepare: tc.err, answer: copiesTheClip})

			tl, err := r.produce(t, localvoice.LipSyncLocal)

			require.NoError(t, err)
			assert.Empty(t, r.rt.calls)
			assert.Empty(t, tl.LipSync)
			sk := skipsOfLipSync(tl)
			require.Len(t, sk, 1, "one entry for the whole runtime, not one for each clip")
			assert.Equal(t, domain.SkipError, sk[0].Reason)
			assert.Contains(t, sk[0].Detail, tc.contains)
			assert.Empty(t, sk[0].Prompt)
			assert.Empty(t, r.cost.costs(domain.ModelMuseTalk))
			assert.Equal(t, 3, spoken(tl), "the speech is made")
			assert.NotEmpty(t, tl.MixPath)
		})
	}
}

// A runtime that goes away between clips (uninstalled in the meantime) fails the same for the rest: the first failure ends the clips.
func TestDirector_ARuntimeThatBecomesUnusableDuringTheRunEndsTheClips(t *testing.T) {
	r := newRig(t, &fakeRuntime{answer: func(ctx context.Context, n int, req lipsync.Request) (lipsync.Response, error) {
		if n == 1 {
			return lipsync.Response{}, &localvoice.UnavailableError{Kind: localvoice.KindNotInstalled, Vendor: localvoice.VendorMuseTalk}
		}
		return copiesTheClip(ctx, n, req)
	}})

	tl, err := r.produce(t, localvoice.LipSyncLocal)

	require.NoError(t, err)
	require.Len(t, r.rt.calls, 2, "the third clip is not tried")
	require.Len(t, tl.LipSync, 1)
	assert.Equal(t, 1, tl.LipSync[0].ClipIndex)
	require.Len(t, skipsOfLipSync(tl), 1)
	assert.Contains(t, skipsOfLipSync(tl)[0].Detail, "導入されていません")
}

func TestDirector_WithoutALocalLipSyncTheRunSaysSoAndKeepsTheAudio(t *testing.T) {
	r := newRig(t, &fakeRuntime{answer: copiesTheClip})
	r.d.Providers.LocalLipSync = nil

	tl, err := r.produce(t, localvoice.LipSyncLocal)

	require.NoError(t, err)
	sk := skipsOfLipSync(tl)
	require.Len(t, sk, 1)
	assert.Contains(t, sk[0].Detail, "ローカルの口パクのプロバイダーが設定されていません")
	assert.Empty(t, r.sync.calls, "the choice of the local provider never falls back to the cloud by itself")
	assert.Empty(t, tl.LipSync)
}

// The caller going away while the runtime starts is the caller's cancel, not "the runtime cannot be used".
func TestDirector_ACancelWhileTheRuntimeStartsIsNotASkippedLipSync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := newRig(t, &fakeRuntime{})
	r.rt.prepare = errors.New("stopped")
	r.d.Providers.LocalLipSync = cancelling{cancel: cancel}

	_, err := r.d.Produce(ctx, audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityMaxQuality, Scenes: threeScenes(), Clips: r.clips, Seconds: 12,
		LipSyncProvider: localvoice.LipSyncLocal,
	})

	require.ErrorIs(t, err, context.Canceled)
}

type cancelling struct{ cancel context.CancelFunc }

func (c cancelling) Prepare(context.Context) error {
	c.cancel()
	return errors.New("stopped")
}

func (cancelling) Sync(context.Context, audiodirector.LipSyncRequest) ([]byte, error) {
	return nil, errors.New("not called")
}

// Below MAX QUALITY the scope does not change: the local provider is no more asked than Sync Labs.
func TestDirector_TheLocalLipSyncIsMaxQualityOnlyLikeSyncLabs(t *testing.T) {
	r := newRig(t, &fakeRuntime{answer: copiesTheClip})

	tl, err := r.d.Produce(context.Background(), audiodirector.Input{
		ProjectID: "p1", EditTimelineID: "e1", Quality: domain.QualityBalanced, Scenes: threeScenes(), Clips: r.clips, Seconds: 12,
		LipSyncProvider: localvoice.LipSyncLocal,
	})

	require.NoError(t, err)
	assert.Empty(t, r.rt.calls)
	assert.Zero(t, r.rt.prepared, "the runtime is not even started")
	assert.Empty(t, tl.LipSync)
	require.Len(t, skipsOfLipSync(tl), 1)
	assert.Equal(t, domain.SkipPolicy, skipsOfLipSync(tl)[0].Reason)
}
