package installtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/install"
)

func TestSpec_AMessageThatDoesNotAskForAnInstallDropsTheModelsOperation(t *testing.T) {
	for _, message := range []string{"この動画のモデルは誰ですか", "ComfyUI って何？", "FLUX の絵が好き", "GPT-SoVITS はどう？", "カットを3秒にして", "huggingface の話"} {
		assert.False(t, install.AsksModel(message) || install.AsksEngine(message) || install.AsksRuntime(message) || install.AsksHF(message), message)
	}
}

// The words of a Brief (a person who is a model, 入れて beside a medium) are no install: the target has to be named as a model.
func TestSpec_TheBriefsOwnWordsAreNoInstall(t *testing.T) {
	for _, message := range []string{"出演者はプロのモデルを入れて", "モデルさんを入れて", "動画に商品を入れて", "参照画像を入れて", "雰囲気は明るい画像にして", "ファッションモデルを入れたい", "女性モデルたちを使って"} {
		assert.False(t, install.AsksModel(message) || install.AsksEngine(message) || install.AsksRuntime(message) || install.AsksHF(message), message)
	}
}

func TestSpec_AMessageThatAsksForAnInstallPassesTheGuard(t *testing.T) {
	assert.True(t, install.AsksModel("FLUX.1 schnell のモデルを入れて"))
	assert.True(t, install.AsksModel("Wan 2.2 を導入して"))
	assert.True(t, install.AsksModel("install the SDXL model"))
	assert.True(t, install.AsksEngine("ComfyUI のエンジンを導入して"))
	assert.True(t, install.AsksRuntime("GPT-SoVITS をインストールしたい"))
	assert.True(t, install.AsksRuntime("MuseTalk を入れて"))
	assert.True(t, install.AsksHF("huggingface.co/owner/name の safetensors をダウンロードして"))
}

func TestSpec_TheOperationsAreRegisteredAsGlobalConfirmedAnyModeOnes(t *testing.T) {
	for _, kind := range []commandinterpreter.OpType{install.OpInstallModel, install.OpInstallEngine, install.OpInstallHFFile, install.OpInstallRuntime} {
		spec, ok := commandinterpreter.SpecOf(kind)
		require.True(t, ok, kind)
		assert.True(t, spec.Global && spec.Confirm && spec.AnyMode, kind)
		assert.NotNil(t, spec.Asked, kind)
		assert.Nil(t, spec.Detect, "an install needs the LLM: nothing is read without it")
		assert.True(t, strings.HasPrefix(spec.Doc, `- "`+string(kind)+`"`) && strings.HasSuffix(spec.Doc, "\n"), kind)
		assert.NotContains(t, spec.Doc+spec.Example, "生成テスト")
	}
}

func TestSpec_CleanKeepsTheNameAndReadsTheMCPArgumentName(t *testing.T) {
	o, ok := commandinterpreter.Normalize(op(t, install.OpInstallModel, map[string]string{"model_id": " flux-schnell "}))

	require.True(t, ok)
	var a install.ModelArgs
	require.NoError(t, o.DecodeArgs(&a))
	assert.Equal(t, install.ModelArgs{Model: "flux-schnell"}, a)

	_, ok = commandinterpreter.Normalize(op(t, install.OpInstallModel, install.ModelArgs{Model: strings.Repeat("a", 301)}))
	assert.False(t, ok, "an answer that long is not a name")
}

func TestProgress_TheLineUnderTheCardSaysHowFarTheInstallIs(t *testing.T) {
	cases := []struct {
		p    install.JobProgress
		want string
	}{
		{install.JobProgress{State: domain.JobRunning, Percent: 42, Phase: "download", DoneBytes: 12 * gb / 10, TotalBytes: 34 * gb / 10}, "42% ・ダウンロード中（1.2 / 3.4 GB）"},
		{install.JobProgress{State: domain.JobRunning, Percent: 95, Phase: "extract", DoneBytes: 300 << 20, TotalBytes: 500 << 20}, "95% ・展開中（300 / 500 MB）"},
		{install.JobProgress{State: domain.JobRunning, Percent: 99, Message: "workflow を登録"}, "99% ・workflow を登録"},
		{install.JobProgress{State: domain.JobRunning}, "0% ・準備中"},
		{install.JobProgress{State: domain.JobQueued}, "順番待ち（まもなく始まります）"},
		{install.JobProgress{State: domain.JobSucceeded, Percent: 100}, "導入が完了しました"},
		{install.JobProgress{State: domain.JobFailed, Error: "ダウンロードが中断されました"}, "導入に失敗しました: ダウンロードが中断されました"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, install.Describe(c.p))
	}
}

func TestProgress_AJobNoSourceKnowsGetsNoDetailSoOtherProvidersCanAnswer(t *testing.T) {
	known := func(_ context.Context, id string) (install.JobProgress, bool) {
		if id != "job-1" {
			return install.JobProgress{}, false
		}
		return install.JobProgress{State: domain.JobRunning, Percent: 10, Phase: "download"}, true
	}
	other := func(context.Context, string) (install.JobProgress, bool) {
		return install.JobProgress{State: domain.JobRunning, Percent: 77, Phase: "extract"}, true
	}
	detail := install.Detail(known)

	assert.Equal(t, "10% ・ダウンロード中", detail(context.Background(), "job-1"))
	assert.Empty(t, detail(context.Background(), "job-unknown"))
	assert.Equal(t, "77% ・展開中", install.Detail(known, other)(context.Background(), "job-2"), "the next source answers when the first does not know the job")
}
