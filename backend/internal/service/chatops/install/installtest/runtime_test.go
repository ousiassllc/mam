package installtest_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/install"
	"github.com/ousiassllc/mam/backend/internal/service/localruntime"
)

type runtimes struct {
	list     []install.RuntimeInfo
	err      error
	installs []string
}

func newRuntimes() *runtimes {
	build := install.Variant{ID: "nvidia", Label: "NVIDIA", Size: 8 * gb, ExtractedSize: 14 * gb, NeedBytes: 22 * gb}
	return &runtimes{list: []install.RuntimeInfo{
		{ID: "gpt-sovits", Name: "GPT-SoVITS", Variant: build, FreeBytes: uint64(100 * gb), WeightsBytes: 3 * gb, WeightsUnclear: 2, License: install.License{Name: "MIT", Commercial: "yes"}},
		{ID: "musetalk", Name: "MuseTalk", Variant: build, FreeBytes: uint64(100 * gb)},
		{ID: "irodori-tts", Name: "Irodori-TTS", Variant: build, FreeBytes: uint64(100 * gb)},
	}}
}

func (r *runtimes) handler() *install.Runtime {
	return install.NewRuntime(install.RuntimeDeps{
		Runtimes: func(context.Context) []install.RuntimeInfo { return r.list },
		Install: func(_ context.Context, id string) (install.Job, error) {
			r.installs = append(r.installs, id)
			return install.Job{ID: "job-r"}, r.err
		},
	})
}

func (r *runtimes) ask(t *testing.T, name string) (chatops.Card, error) {
	t.Helper()
	return r.handler().Question(context.Background(), project, op(t, install.OpInstallRuntime, install.RuntimeArgs{Runtime: name}))
}

func TestRuntime_TheCardShowsTheDownloadAndExtractedSizeAndTheWeightsAndInstallsNothing(t *testing.T) {
	r := newRuntimes()

	for _, said := range []string{"gpt-sovits", "GPT-SoVITS", "gpt sovits"} {
		card, err := r.ask(t, said)

		require.NoError(t, err, said)
		for _, want := range []string{"GPT-SoVITS", "約 8.0 GB", "展開後 約 14.0 GB", "必要な空き容量 22.0 GB", "重み・依存ファイルは 3.0 GB", "2 件", "全プロジェクト"} {
			assert.Contains(t, card.Text, want, said)
		}
		var a install.RuntimeArgs
		require.NoError(t, yesOp(t, op(t, install.OpInstallRuntime, install.RuntimeArgs{Runtime: said}), card).DecodeArgs(&a))
		assert.Equal(t, "gpt-sovits", a.Runtime, "the yes carries the id")
	}
	assert.Empty(t, r.installs)
}

func TestRuntime_ANameThatMatchesSeveralAsksWhichAndAnUnknownOneIsRefused(t *testing.T) {
	r := newRuntimes()
	r.list[1].Name, r.list[2].Name = "Voice A", "Voice B"

	card, err := r.ask(t, "voice")
	require.NoError(t, err)
	assert.Len(t, card.Choices, 2)
	for _, c := range card.Choices {
		var a install.RuntimeArgs
		require.NoError(t, c.Op.DecodeArgs(&a))
		assert.True(t, a.Pick)
	}

	_, err = r.ask(t, "does-not-exist")
	assert.Contains(t, refusal(t, err), "「does-not-exist」という実行環境はありません")
	_, err = r.ask(t, "")
	assert.Contains(t, refusal(t, err), "どの実行環境を導入しますか")
	assert.Empty(t, r.installs)
}

func TestRuntime_ARuntimeThisMachineCannotRunIsRefusedWithItsReasonAndNoCard(t *testing.T) {
	r := newRuntimes()
	r.list[0].Unsupported = "Windows と NVIDIA の GPU が必要です。クラウドの ElevenLabs を使ってください"

	card, err := r.ask(t, "gpt-sovits")

	assert.Contains(t, refusal(t, err), "ElevenLabs")
	assert.Empty(t, card.Text)
}

func TestRuntime_RefusalsAndNoChange(t *testing.T) {
	r := newRuntimes()
	r.list[0].InstallJob = "job-4"
	_, err := r.ask(t, "gpt-sovits")
	assert.Contains(t, refusal(t, err), "すでに導入中")

	r = newRuntimes()
	r.list[0].NotWritable = "read-only"
	_, err = r.ask(t, "gpt-sovits")
	assert.Contains(t, refusal(t, err), "書き込めません")

	r = newRuntimes()
	r.list[0].FreeBytes = uint64(gb)
	_, err = r.ask(t, "gpt-sovits")
	assert.Contains(t, refusal(t, err), "空き容量が足りません（必要 22.0 GB、空き 1.0 GB）")

	r = newRuntimes()
	r.list[0].Installed = true
	_, err = r.ask(t, "gpt-sovits")
	assert.ErrorIs(t, err, chatops.ErrNoChange)

	r.list[0].Updates, r.list[0].Version, r.list[0].PinnedVersion = true, "1", "2"
	card, err := r.ask(t, "gpt-sovits")
	require.NoError(t, err)
	assert.Contains(t, card.Text, "更新しますね？（導入済み 1 → 2）")
}

func TestRuntime_ApplyQueuesTheInstallOnceAndShowsTheJob(t *testing.T) {
	r := newRuntimes()

	res, err := r.handler().Apply(context.Background(), project, op(t, install.OpInstallRuntime, install.RuntimeArgs{Runtime: "gpt-sovits"}))

	require.NoError(t, err)
	assert.Equal(t, []string{"gpt-sovits"}, r.installs)
	requireProgressCard(t, res, "job-r")
	assert.Contains(t, res.Messages[0].Content, "GPT-SoVITS")
}

func TestRuntime_ApplyOfAnIDThatIsNotInTheCatalogInstallsNothing(t *testing.T) {
	r := newRuntimes()

	_, err := r.handler().Apply(context.Background(), project, op(t, install.OpInstallRuntime, install.RuntimeArgs{Runtime: "rm-rf"}))

	require.Error(t, err)
	assert.Empty(t, r.installs)
}

func TestRuntime_ApplyTellsTheRefusalsOfTheService(t *testing.T) {
	r := newRuntimes()
	r.err = localruntime.ErrBusy

	res, err := r.handler().Apply(context.Background(), project, op(t, install.OpInstallRuntime, install.RuntimeArgs{Runtime: "gpt-sovits"}))

	assert.Contains(t, refusal(t, err), "導入が実行中です")
	assert.Empty(t, res.Messages)
}

func TestRuntime_PickingACandidateAsksItsConfirmationAndInstallsNothing(t *testing.T) {
	r := newRuntimes()

	res, err := r.handler().Apply(context.Background(), project, op(t, install.OpInstallRuntime, install.RuntimeArgs{Runtime: "musetalk", Pick: true}))

	require.NoError(t, err)
	assert.Empty(t, r.installs)
	require.Len(t, res.Messages, 1)
	assert.Contains(t, res.Messages[0].Content, "MuseTalk")
}

// A name that fits more runtimes than a card holds as buttons asks to be narrowed down, with no button; as many as the card holds are asked as buttons.
func TestRuntime_TooManyRuntimesAreAskedToBeNarrowed(t *testing.T) {
	r := newRuntimes()
	for i := range chatops.MaxChoices + 1 {
		r.list = append(r.list, install.RuntimeInfo{ID: fmt.Sprintf("mega-%d", i), Name: fmt.Sprintf("Mega %d", i)})
	}

	card, err := r.ask(t, "mega")

	assert.Contains(t, refusal(t, err), "7 件")
	assert.Empty(t, card.Choices)

	r.list = r.list[:len(r.list)-1]
	card, err = r.ask(t, "mega")
	require.NoError(t, err)
	assert.Len(t, card.Choices, chatops.MaxChoices)
}

// Files that share the Name given are asked as buttons up to the card limit; more are asked to be narrowed down by Path, with no button.
func TestHF_TooManyFilesWithTheSameNameAreAskedToBeNarrowed(t *testing.T) {
	h := newHub()
	h.repo.Files = nil
	for i := range chatops.MaxChoices + 1 {
		h.repo.Files = append(h.repo.Files, install.HFFile{Path: fmt.Sprintf("d%d/model.safetensors", i), Name: "model.safetensors", Size: gb, Installable: true, Folder: "loras"})
	}

	card, err := h.ask(t, install.HFArgs{Repo: "owner/style", Path: "model.safetensors"})

	assert.Contains(t, refusal(t, err), "7 件")
	assert.Contains(t, refusal(t, err), "フォルダ込みのパス（例: d0/model.safetensors）")
	assert.Empty(t, card.Choices)

	// a file of that name that cannot be installed is no button and does not count toward the limit
	h.repo.Files = append(h.repo.Files[:chatops.MaxChoices], install.HFFile{Path: "x/model.safetensors", Name: "model.safetensors", Size: gb})
	card, err = h.ask(t, install.HFArgs{Repo: "owner/style", Path: "model.safetensors"})
	require.NoError(t, err)
	assert.Len(t, card.Choices, chatops.MaxChoices)

	// and the one installable file among them is that file
	h.repo.Files = []install.HFFile{h.repo.Files[0], h.repo.Files[chatops.MaxChoices]}
	card, err = h.ask(t, install.HFArgs{Repo: "owner/style", Path: "model.safetensors"})
	require.NoError(t, err)
	require.Len(t, card.Choices, 1)
	var a install.HFArgs
	require.NoError(t, card.Choices[0].Op.DecodeArgs(&a))
	assert.Equal(t, "d0/model.safetensors", a.Path)
}
