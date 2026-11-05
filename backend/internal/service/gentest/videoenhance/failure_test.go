package videoenhance_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/videoenhance"
)

// queued stores a result of an enhancement that nothing has run yet, with the values ve says, and runs it now (as the prompt match does).
func queued(t *testing.T, r *gentesttest.Rig, model string, ve record.VideoEnhance) gentestrepo.Result {
	t.Helper()
	id := domain.NewID()
	params, err := json.Marshal(record.ResultParams{VideoEnhance: &ve})
	require.NoError(t, err)
	res := gentestrepo.Result{ID: id, BatchID: id, Media: gentestrepo.MediaVideo, Model: model, Params: params, NegativeMode: gentestrepo.NegativeNone, CostBasis: "unknown", CreatedAt: time.Now().UTC()}
	require.NoError(t, r.Repo.CreateBatch(ctx, gentestrepo.NewBatch{Batch: gentestrepo.Batch{ID: id, Media: gentestrepo.MediaVideo, CreatedAt: res.CreatedAt}, Results: []gentestrepo.Result{res}}))
	require.NoError(t, os.MkdirAll(filepath.Join(r.DataDir, gentest.DirName, id), 0o750))
	r.Svc.Generate(ctx, res)
	got, err := r.Repo.GetResult(ctx, id)
	require.NoError(t, err)
	return got
}

// A failure the app words itself says why and what to do in Japanese; the report must not map it to "unknown" (原因がわかりませんでした).
func TestACloudEnhancementThatCannotRunSaysWhyInJapanese(t *testing.T) {
	local := string(gentesttest.UpscaleVideoModel)
	for name, tc := range map[string]struct {
		cloud   *gentesttest.FakeCloud
		model   string
		ve      func(src string) record.VideoEnhance
		deleted func(t *testing.T, r *gentesttest.Rig, src string)
		code    apperr.Code
		cause   string
	}{
		"the source result is gone": {
			model: local, ve: func(string) record.VideoEnhance {
				return record.VideoEnhance{SourceResultID: "gone", Op: "upscale", Provider: "local", Scale: 2}
			},
			code: apperr.CodeNotFound, cause: "元の動画の結果が削除された",
		},
		"the source file is gone": {
			model: local, ve: func(src string) record.VideoEnhance {
				return record.VideoEnhance{SourceResultID: src, Op: "upscale", Provider: "local", Scale: 2, SourceWidth: 480, SourceHeight: 848, SourceFPS: 16}
			},
			deleted: func(t *testing.T, r *gentesttest.Rig, src string) {
				res, err := r.Repo.GetResult(ctx, src)
				require.NoError(t, err)
				require.NoError(t, os.Remove(filepath.Join(r.DataDir, gentest.DirName, filepath.FromSlash(res.FilePath))))
			},
			code: apperr.CodeNotFound, cause: "元の動画のファイルを読み込めませんでした",
		},
		"this app has no cloud": {
			model: videoenhance.CloudPrefix + "prob-4", ve: func(src string) record.VideoEnhance {
				return record.VideoEnhance{SourceResultID: src, Op: "upscale", Provider: "topaz", Scale: 2, Topaz: &record.TopazRun{RequestID: "req-1"}}
			},
			code: apperr.CodeUnavailable, cause: "クラウドの動画処理（Topaz）が使えません",
		},
		"the quote was not kept": {
			cloud: newCloud(true), model: videoenhance.CloudPrefix + "prob-4", ve: func(src string) record.VideoEnhance {
				return record.VideoEnhance{SourceResultID: src, Op: "upscale", Provider: "topaz", Scale: 2}
			},
			code: apperr.CodeInvalidInput, cause: "見積りの情報がこの結果に残っていない",
		},
	} {
		t.Run(name, func(t *testing.T) {
			opts := []gentesttest.Option{}
			if tc.cloud != nil {
				opts = append(opts, gentesttest.WithCloud(tc.cloud))
			}
			r := newRig(t, opts...)
			src := aVideo(t, r)
			if tc.deleted != nil {
				tc.deleted(t, r, src)
			}

			got := queued(t, r, tc.model, tc.ve(src))

			require.Equal(t, "failed", got.Status)
			assert.Contains(t, got.Error, tc.cause)
			assert.NotContains(t, got.Error, "原因がわかりませんでした")
			e, ok := apperr.Unmarshal(got.ErrorInfo)
			require.True(t, ok, "the failure is kept as the common error")
			assert.Equal(t, tc.code, e.Code)
			assert.Contains(t, e.View(apperr.LangJa).Cause, tc.cause)
			if tc.cloud != nil {
				assert.Empty(t, tc.cloud.Runs)
			}
		})
	}
}

// A job that Topaz accepted goes on there when mam stops: the result says so with the request id, instead of the plain "interrupted".
func TestAnInterruptedCloudJobTellsTheRequestIDToLookItUpBy(t *testing.T) {
	cloud := newCloud(true)
	cloud.HoldRun, cloud.RunStarted = true, make(chan struct{}, 1)
	r := newRig(t, gentesttest.WithCloud(cloud))
	src := aVideo(t, r)
	est, err := videoenhance.EstimateEnhance(ctx, r.Svc, upscale(src, proteus))
	require.NoError(t, err)
	req := upscale(src, proteus)
	req.EstimateID, req.ConfirmedCredits = est.EstimateID, *est.Credits
	ref, err := videoenhance.Enhance(ctx, r.Svc, req)
	require.NoError(t, err)
	select {
	case <-cloud.RunStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("the cloud job did not start")
	}

	r.Queue.Stop() // mam stops while Topaz works

	got, err := r.Repo.GetResult(context.Background(), ref.ResultID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	assert.NotContains(t, got.Error, "mam が停止したため中断されました（課金される生成は再開しません）")
	assert.Contains(t, got.Error, "req-1", "the request id to look the job up by")
	assert.Contains(t, got.Error, "Topaz")
	assert.Contains(t, got.Error, "クレジット")
	assert.Contains(t, got.Error, "取り消せません")
	e, ok := apperr.Unmarshal(got.ErrorInfo)
	require.True(t, ok)
	assert.Equal(t, apperr.CodeTopazInterrupted, e.Code)
	assert.Len(t, cloud.Runs, 1, "never submitted again")
}
