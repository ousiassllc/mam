package installtest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/install"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

type engine struct {
	info     install.EngineInfo
	installs []engineInstall
	err      error
}

type engineInstall struct {
	Variant string
	CPU     bool
}

func newEngine() *engine {
	return &engine{info: install.EngineInfo{
		Supported: true, Writable: true, FreeBytes: uint64(100 * gb), LicenseName: "GPL-3.0", RecommendedVariant: "nvidia",
		Variants: []install.Variant{
			{ID: "nvidia", Label: "NVIDIA（RTX 20 シリーズ以降）", Size: 2 * gb, ExtractedSize: 5 * gb, NeedBytes: 7 * gb},
			{ID: "amd", Label: "AMD", Size: gb, ExtractedSize: 3 * gb, NeedBytes: 4 * gb},
		},
	}}
}

func (e *engine) handler() *install.Engine {
	return install.NewEngine(install.EngineDeps{
		Engine: func(context.Context) (install.EngineInfo, error) { return e.info, nil },
		Install: func(_ context.Context, variant string, cpu bool) (install.Job, error) {
			e.installs = append(e.installs, engineInstall{variant, cpu})
			return install.Job{ID: "job-e"}, e.err
		},
	})
}

func (e *engine) ask(t *testing.T, args install.EngineArgs) (chatops.Card, error) {
	t.Helper()
	return e.handler().Question(context.Background(), project, op(t, install.OpInstallEngine, args))
}

func TestEngine_TheCardShowsTheBuildOfTheGPUWithItsDownloadAndExtractedSizeAndInstallsNothing(t *testing.T) {
	e := newEngine()

	card, err := e.ask(t, install.EngineArgs{})

	require.NoError(t, err)
	for _, want := range []string{"ComfyUI エンジン", "NVIDIA（RTX 20 シリーズ以降）", "約 2.0 GB", "展開後 約 5.0 GB", "必要な空き容量 7.0 GB", "GPL-3.0", "全プロジェクト"} {
		assert.Contains(t, card.Text, want)
	}
	assert.Equal(t, install.EngineArgs{Variant: "nvidia"}, decodeEngine(t, yesOp(t, op(t, install.OpInstallEngine, install.EngineArgs{}), card)), "the yes carries the build the card showed")
	assert.Empty(t, e.installs)
}

func decodeEngine(t *testing.T, o interface{ DecodeArgs(any) error }) install.EngineArgs {
	t.Helper()
	var a install.EngineArgs
	require.NoError(t, o.DecodeArgs(&a))
	return a
}

func TestEngine_TheCPUIsOnlyUsedWhenAskedFor(t *testing.T) {
	e := newEngine()
	e.info.RecommendedVariant, e.info.RecommendationUnavailable = "", "対応する GPU が見つかりません。外部の ComfyUI の URL を設定するか、CPU 実行を明示的に選んでください"

	_, err := e.ask(t, install.EngineArgs{})
	assert.Contains(t, refusal(t, err), "対応する GPU が見つかりません")

	card, err := e.ask(t, install.EngineArgs{CPU: true})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "CPU で動かします")
	assert.True(t, decodeEngine(t, yesOp(t, op(t, install.OpInstallEngine, install.EngineArgs{CPU: true}), card)).CPU)
}

func TestEngine_RefusalsAreToldBeforeAnyCard(t *testing.T) {
	cases := map[string]struct {
		change func(*engine)
		want   string
	}{
		"an OS without the engine": {func(e *engine) { e.info.Supported = false }, "Windows 版と Apple Silicon"},
		"a pinned file":            {func(e *engine) { e.info.Pinned = true }, "--model-config"},
		"a folder it cannot write": {func(e *engine) { e.info.Writable = false }, "書き込めません"},
		"no room":                  {func(e *engine) { e.info.FreeBytes = uint64(gb) }, "空き容量が足りません（必要 7.0 GB、空き 1.0 GB）"},
		"an install running":       {func(e *engine) { e.info.InstallJob = "job-3" }, "すでに導入中"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEngine()
			c.change(e)

			card, err := e.ask(t, install.EngineArgs{})

			assert.Contains(t, refusal(t, err), c.want)
			assert.Empty(t, card.Text)
			assert.Empty(t, e.installs)
		})
	}
}

func TestEngine_AnEngineThatIsUpToDateChangesNothingAndAnOldOneIsUpdated(t *testing.T) {
	e := newEngine()
	e.info.Installed, e.info.Version, e.info.PinnedVersion = true, "v1", "v1"

	_, err := e.ask(t, install.EngineArgs{})
	assert.ErrorIs(t, err, chatops.ErrNoChange)

	e.info.UpdateAvailable, e.info.PinnedVersion = true, "v2"
	card, err := e.ask(t, install.EngineArgs{})
	require.NoError(t, err)
	assert.Contains(t, card.Text, "更新しますね？（導入済み v1 → v2）")
}

func TestEngine_ApplyQueuesTheBuildOfTheCardOnceAndShowsTheJob(t *testing.T) {
	e := newEngine()

	res, err := e.handler().Apply(context.Background(), project, op(t, install.OpInstallEngine, install.EngineArgs{Variant: "amd"}))

	require.NoError(t, err)
	assert.Equal(t, []engineInstall{{Variant: "amd"}}, e.installs)
	requireProgressCard(t, res, "job-e")
	assert.Contains(t, res.Messages[0].Content, "ComfyUI エンジン")
}

func TestEngine_ApplyTellsTheRefusalsOfTheService(t *testing.T) {
	e := newEngine()
	e.err = modelmanager.ErrBusy

	res, err := e.handler().Apply(context.Background(), project, op(t, install.OpInstallEngine, install.EngineArgs{}))

	assert.Contains(t, refusal(t, err), "導入または試し生成が実行中です")
	assert.Empty(t, res.Messages)
}
