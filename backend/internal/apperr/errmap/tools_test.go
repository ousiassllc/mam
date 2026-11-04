package errmap_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/audiodirector"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/apperr/errmap"
	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/media"
)

// The failures of the tools mam starts (ffmpeg, FaceFusion) are told as the common error: a cause and a remedy in Japanese, and the English
// message the tool gave stays as the detail (#1010).
func TestExplain_ClassifiesTheFailuresOfTheExternalTools(t *testing.T) {
	cfg := &facefusion.ConfigError{Field: facefusion.FieldPython, Path: "/no/python", Err: os.ErrNotExist}
	for name, tc := range map[string]struct {
		err    error
		want   apperr.Code
		cause  string
		remedy string
		link   string
	}{
		"no ffmpeg":            {fmt.Errorf("render: %w", media.ErrUnavailable), apperr.CodeFFmpegMissing, "ffmpeg と ffprobe が見つかりません", "設定画面の「ffmpeg」", "/settings/ffmpeg"},
		"ffmpeg timeout":       {fmt.Errorf("render: %w after 2m", media.ErrTimeout), apperr.CodeMediaTimeout, "ffmpeg）が制限時間を超えました", "素材ファイル", ""},
		"facefusion not found": {fmt.Errorf("facefusion %w (searched /home/me)", facefusion.ErrNotFound), apperr.CodeFaceFusionNone, "FaceFusion を使えません", "同梱していない", "/settings/facefusion"},
		"facefusion bad path":  {cfg, apperr.CodeFaceFusionNone, "FaceFusion を使えません", "パスを確かめて", "/settings/facefusion"},
		"facefusion exit":      {&facefusion.RunError{Err: errors.New("exit status 1"), Output: "Traceback"}, apperr.CodeFaceFusionFailed, "FaceFusion が途中でエラーを出して終わりました", "顔がはっきり写っているか", "/settings/facefusion"},
		"facefusion timeout":   {&facefusion.RunError{Err: context.DeadlineExceeded}, apperr.CodeFaceFusionFailed, "決められた時間内に終わらなかった", "成人向けの画像", "/settings/facefusion"},
		"facefusion no output": {&facefusion.RunError{}, apperr.CodeFaceFusionFailed, "出力の画像がありません", "FaceFusion が断ります", "/settings/facefusion"},
		"facefusion GPU":       {&facefusion.RunError{Err: errors.New("exit status 1"), Output: "torch.OutOfMemoryError: CUDA out of memory"}, apperr.CodeVRAM, "VRAM", "解像度", ""},
	} {
		t.Run(name, func(t *testing.T) {
			e := errmap.Explain(tc.err)
			require.NotNil(t, e)
			assert.Equal(t, tc.want, e.Code)
			assert.ErrorIs(t, e, tc.err)
			v := e.View(apperr.LangJa)
			assert.Contains(t, v.Cause, tc.cause)
			assert.Contains(t, v.Remedy, tc.remedy)
			assert.NotContains(t, v.Cause+v.Remedy, "{")
			assert.NotEmpty(t, v.Detail, "what the tool said stays as the detail")
			if tc.link == "" {
				assert.Empty(t, v.Settings)
			} else {
				require.NotEmpty(t, v.Settings)
				assert.Equal(t, tc.link, v.Settings[0].Path)
			}
		})
	}
}

// Media tells a failure of ffmpeg: what is about the machine is told as it is, the rest is media_failed with what ffmpeg printed as the detail.
func TestMedia_TellsWhatHappenedToTheMachineAndKeepsWhatFFmpegSaid(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want apperr.Code
	}{
		"no ffmpeg":   {media.ErrUnavailable, apperr.CodeFFmpegMissing},
		"timeout":     {fmt.Errorf("%w after 2m0s", media.ErrTimeout), apperr.CodeMediaTimeout},
		"disk":        {fmt.Errorf("write: %w", syscall.ENOSPC), apperr.CodeDiskFull},
		"permission":  {fmt.Errorf("open: %w", os.ErrPermission), apperr.CodeNotWritable},
		"ffmpeg exit": {errors.New("ffmpeg: exit status 1: Invalid data found when processing input"), apperr.CodeMediaFailed},
		// The messages ComfyUI's GPU failures are known by say nothing of ffmpeg's own memory.
		"ffmpeg memory": {errors.New("ffmpeg: Cannot allocate memory: out of memory"), apperr.CodeMediaFailed},
	} {
		t.Run(name, func(t *testing.T) {
			e := errmap.Media("what.export", tc.err)
			require.NotNil(t, e)
			assert.Equal(t, tc.want, e.Code)
			assert.ErrorIs(t, e, tc.err)
			assert.NotEmpty(t, e.View(apperr.LangJa).Remedy)
		})
	}
	e := errmap.Media("what.export", errors.New("ffmpeg: exit status 1"))
	assert.Contains(t, e.View(apperr.LangJa).Cause, "動画の書き出しに失敗しました")
	assert.Contains(t, e.View(apperr.LangEn).Cause, "Exporting the video failed")
	assert.Equal(t, "ffmpeg: exit status 1", e.Detail)
	assert.Nil(t, errmap.Media("what.export", nil))
}

// A failed save is save_failed with the disk or the permission as the reason when it is one, and the failure as the detail; the screens of the
// settings and the tools of the MCP both use it, so that one failure reads the same on both. The remedies of the model manager (about models) are
// not in it.
func TestSaveFailure_SaysSaveFailedWithTheReasonItKnows(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want string
	}{
		"disk full":    {fmt.Errorf("write settings: %w", syscall.ENOSPC), "（ディスクの空き容量が足りません）"},
		"permission":   {fmt.Errorf("write settings: %w", os.ErrPermission), "（保存先のフォルダに書き込めません）"},
		"other":        {errors.New("rename /data/x.json: invalid cross-device link"), "保存できませんでした。"},
		"unknown kind": {errors.New("boom"), "保存できませんでした。"},
	} {
		t.Run(name, func(t *testing.T) {
			e := errmap.SaveFailure(tc.err)
			require.NotNil(t, e)
			assert.Equal(t, apperr.CodeSaveFailed, e.Code)
			v := e.View(apperr.LangJa)
			assert.Contains(t, v.Cause, tc.want)
			assert.Contains(t, v.Remedy, "ディスクの空き容量とフォルダの権限を確かめて")
			assert.NotContains(t, v.Cause+v.Remedy, "モデル")
			assert.Empty(t, v.Settings)
			assert.ErrorIs(t, e, tc.err)
		})
	}
	told := apperr.New(apperr.CodeConflict).WithCause("固定されています。")
	assert.Same(t, told, errmap.SaveFailure(told), "a failure the app worded is not changed")
	assert.Contains(t, errmap.SaveFailure(errors.New("x: boom")).Detail, "boom")
	assert.Contains(t, errmap.SaveFailure(os.ErrPermission).View(apperr.LangEn).Cause, "(the destination folder cannot be written)")
	assert.Nil(t, errmap.SaveFailure(nil))
}

// The English texts of every code errmap makes: the cause and the remedy are in English, a limit of time is told in the unit of the language
// and a place of the disk keeps its path, so that an English screen is English from the first word.
func TestExplain_TheEnglishTextsOfTheCodesErrmapMakes(t *testing.T) {
	for name, tc := range map[string]struct {
		err   error
		cause string
	}{
		"stalled":  {localcfg.MarkStall(30*time.Minute, errors.New("t")), "showed no progress for 30 min"},
		"hours":    {&localcfg.TimeoutError{After: localcfg.MaxTimeoutSeconds * time.Second, Err: errors.New("t")}, "overall limit (24 h)"},
		"seconds":  {&localcfg.TimeoutError{After: 90 * time.Second, Err: errors.New("t")}, "waiting time of its endpoint (90 s)"},
		"ffmpeg":   {media.ErrUnavailable, "ffmpeg and ffprobe were not found"},
		"facefuse": {&facefusion.RunError{}, "FaceFusion finished, but there is no output image"},
	} {
		t.Run(name, func(t *testing.T) {
			e := errmap.Explain(tc.err)
			require.NotNil(t, e)
			assert.Contains(t, e.View(apperr.LangEn).Cause, tc.cause)
			assert.Contains(t, e.View(apperr.LangJa).Cause, "。", "the Japanese is the base and says its own sentence")
		})
	}
	key := errmap.Explain(fmt.Errorf("x: %w", audiodirector.ErrNoAPIKey))
	assert.Contains(t, key.View(apperr.LangEn).Cause, "ElevenLabs API key")
	assert.Contains(t, key.View(apperr.LangJa).Cause, "ElevenLabs の API キー")
}

// Each kind of failed update check is one code, with a cause and a remedy in Japanese that point at the screen of the update (and in English).
func TestUpdateCheck_EveryKindIsTheCommonErrorOfItsCode(t *testing.T) {
	for kind, want := range map[string]struct {
		code   apperr.Code
		cause  string
		remedy string
		en     string
	}{
		"network":                   {apperr.CodeUpdateNetwork, "ネットワークに接続できませんでした", "ネットワークの接続を確かめて", "could not connect to the network"},
		"rate_limit":                {apperr.CodeUpdateRateLimit, "取得回数の上限に達した", "自動でもう一度確認", "request limit of GitHub"},
		"access":                    {apperr.CodeUpdateAccess, "アクセスが拒否されたか", "ファイアウォール", "access was refused"},
		"verification":              {apperr.CodeUpdateVerify, "検証できませんでした", "入れ直して", "could not be verified"},
		"release":                   {apperr.CodeUpdateRelease, "リリースの内容が正しくありません", "配布元に知らせて", "content of the published release"},
		"other":                     {apperr.CodeUpdateFailed, "原因はわかりませんでした", "ログファイル", "reason that is not known"},
		"a kind of a later version": {apperr.CodeUpdateFailed, "原因はわかりませんでした", "ログファイル", "reason that is not known"},
	} {
		t.Run(kind, func(t *testing.T) {
			e := errmap.UpdateCheck(kind)

			assert.Equal(t, want.code, e.Code)
			v := e.View(apperr.LangJa)
			assert.Contains(t, v.Cause, want.cause)
			assert.Contains(t, v.Remedy, want.remedy)
			require.Len(t, v.Settings, 1)
			assert.Equal(t, "/settings/update", v.Settings[0].Path)
			assert.Empty(t, v.Detail, "the raw message of the check stays in the log")
			assert.Contains(t, e.View(apperr.LangEn).Cause, want.en)
		})
	}
}
