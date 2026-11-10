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

const pinned = "0123456789abcdef0123456789abcdef01234567"

type hub struct {
	repo     install.HFRepo
	env      install.Environment
	err      error
	asked    []string
	installs []hfInstall
}

type hfInstall struct{ Repo, Commit, Path, Terms string }

func newHub() *hub {
	return &hub{env: home, repo: install.HFRepo{
		ID: "owner/style", Commit: pinned, Page: "https://huggingface.co/owner/style", Terms: "取得するのはあなたの判断です。", TermsVersion: "1", TokenConfigured: true,
		License: install.License{Name: "apache-2.0", Commercial: "conditional", Condition: "モデルカードの自己申告です", URL: "https://example.com/license"},
		Files: []install.HFFile{
			{Path: "style.safetensors", Name: "style.safetensors", Size: 2 * gb, Installable: true, Folder: "loras", Security: "safe"},
		},
	}}
}

func (h *hub) handler() *install.HF {
	return install.NewHF(install.HFDeps{
		Environment: func(context.Context) (install.Environment, error) { return h.env, nil },
		Repo: func(_ context.Context, input string) (install.HFRepo, error) {
			h.asked = append(h.asked, input)
			return h.repo, h.err
		},
		Install: func(_ context.Context, repo, commit, path, terms string) (install.Job, error) {
			h.installs = append(h.installs, hfInstall{repo, commit, path, terms})
			return install.Job{ID: "job-h"}, nil
		},
	})
}

func (h *hub) ask(t *testing.T, args install.HFArgs) (chatops.Card, error) {
	t.Helper()
	return h.handler().Question(context.Background(), project, op(t, install.OpInstallHFFile, args))
}

func TestHF_TheCardShowsTheFileTheCommitTheLicenseAndTheTermsAndTheButtonIsTheConsent(t *testing.T) {
	h := newHub()

	card, err := h.ask(t, install.HFArgs{Repo: "owner/style"})

	require.NoError(t, err)
	for _, want := range []string{"owner/style", "style.safetensors", "2.0 GB", "models/loras/style.safetensors", "0123456（", "モデルカードの自己申告", "商用利用: 条件つき", "https://example.com/license", "取得するのはあなたの判断です", "版 1", "同意して導入する", "全プロジェクト"} {
		assert.Contains(t, card.Text, want)
	}
	require.Len(t, card.Choices, 1)
	var a install.HFArgs
	require.NoError(t, card.Choices[0].Op.DecodeArgs(&a))
	assert.Equal(t, install.HFArgs{Repo: "owner/style", Path: "style.safetensors", Commit: pinned, TermsVersion: "1"}, a)
	assert.Empty(t, h.installs)
}

func TestHF_TheCommitIsTheOneTheHubGivesNeverOneTheModelWrote(t *testing.T) {
	h := newHub()

	card, err := h.ask(t, install.HFArgs{Repo: "owner/style", Commit: "ffffffffffffffffffffffffffffffffffffffff", TermsVersion: "99"})

	require.NoError(t, err)
	var a install.HFArgs
	require.NoError(t, card.Choices[0].Op.DecodeArgs(&a))
	assert.Equal(t, pinned, a.Commit)
	assert.Equal(t, "1", a.TermsVersion)
	assert.Equal(t, []string{"owner/style"}, h.asked)
}

func TestHF_TermsConfirmedForThisCommitNeedNoNewConsent(t *testing.T) {
	h := newHub()
	h.repo.Consented = true

	card, err := h.ask(t, install.HFArgs{Repo: "owner/style"})

	require.NoError(t, err)
	assert.Empty(t, card.Choices)
	got := yesOp(t, op(t, install.OpInstallHFFile, install.HFArgs{Repo: "owner/style"}), card)
	var a install.HFArgs
	require.NoError(t, got.DecodeArgs(&a))
	assert.Equal(t, install.HFArgs{Repo: "owner/style", Path: "style.safetensors", Commit: pinned}, a, "no terms version: nothing is accepted by this yes")
}

func TestHF_SeveralFilesAskWhichAndPickingOneAsksItsConfirmation(t *testing.T) {
	h := newHub()
	h.repo.Files = append(h.repo.Files, install.HFFile{Path: "alt/style-fp8.safetensors", Name: "style-fp8.safetensors", Size: gb, Installable: true, Folder: "loras"})

	card, err := h.ask(t, install.HFArgs{Repo: "owner/style"})

	require.NoError(t, err)
	require.Len(t, card.Choices, 2)
	var a install.HFArgs
	require.NoError(t, card.Choices[1].Op.DecodeArgs(&a))
	assert.Equal(t, install.HFArgs{Repo: "owner/style", Path: "alt/style-fp8.safetensors", Pick: true}, a)
	assert.Empty(t, h.installs)

	res, err := h.handler().Apply(context.Background(), project, card.Choices[1].Op)
	require.NoError(t, err)
	assert.Empty(t, h.installs, "picking installs nothing")
	require.Len(t, res.Messages, 1)
	assert.Contains(t, res.Messages[0].Content, "alt/style-fp8.safetensors")
	assert.Contains(t, res.Messages[0].Content, "同意して導入する")
}

func TestHF_ARepositoryThatCannotBeInstalledIsRefusedBeforeAnyCard(t *testing.T) {
	cases := map[string]struct {
		change func(*hub)
		args   install.HFArgs
		want   string
	}{
		"no repository named":      {func(*hub) {}, install.HFArgs{}, "どの Hugging Face のリポジトリですか"},
		"no engine":                {func(h *hub) { h.env.EngineInstalled = false }, install.HFArgs{Repo: "owner/style"}, "先に ComfyUI エンジンを導入してください"},
		"a pinned settings file":   {func(h *hub) { h.env.Pinned = true }, install.HFArgs{Repo: "owner/style"}, "--model-config"},
		"gated without a token":    {func(h *hub) { h.repo.Gated, h.repo.TokenConfigured = true, false }, install.HFArgs{Repo: "owner/style"}, "トークンを設定"},
		"a file that is not there": {func(*hub) {}, install.HFArgs{Repo: "owner/style", Path: "nope.safetensors"}, "「nope.safetensors」というファイルはありません"},
		"a file mam cannot install": {func(h *hub) {
			h.repo.Files[0].Installable, h.repo.Files[0].Reason = false, "pickle 形式は導入できません"
		}, install.HFArgs{Repo: "owner/style"}, "pickle 形式は導入できません"},
		"a folder mam cannot decide": {func(h *hub) { h.repo.Files[0].Folder = "" }, install.HFArgs{Repo: "owner/style"}, "フォルダを決められませんでした"},
		"no room":                    {func(h *hub) { h.env.FreeBytes = uint64(gb) }, install.HFArgs{Repo: "owner/style"}, "空き容量が足りません"},
		"an install running":         {func(h *hub) { h.repo.Files[0].InstallJob = "job-2" }, install.HFArgs{Repo: "owner/style"}, "すでに導入中"},
		"the adult gate":             {func(h *hub) { h.err = modelmanager.ErrAdultNotConfirmed }, install.HFArgs{Repo: "owner/style"}, "年齢の確認"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHub()
			c.change(h)

			card, err := h.ask(t, c.args)

			assert.Contains(t, refusal(t, err), c.want)
			assert.Empty(t, card.Text)
			assert.Empty(t, h.installs)
		})
	}
}

func TestHF_AFileThatIsInstalledAlreadyChangesNothing(t *testing.T) {
	h := newHub()
	h.repo.Files[0].Installed = true

	_, err := h.ask(t, install.HFArgs{Repo: "owner/style"})

	assert.ErrorIs(t, err, chatops.ErrNoChange)
}

func TestHF_ApplyPassesTheCommitAndTheTermsVersionOfTheCardAndShowsTheJob(t *testing.T) {
	h := newHub()

	res, err := h.handler().Apply(context.Background(), project, op(t, install.OpInstallHFFile, install.HFArgs{Repo: "owner/style", Path: "style.safetensors", Commit: pinned, TermsVersion: "1"}))

	require.NoError(t, err)
	assert.Equal(t, []hfInstall{{"owner/style", pinned, "style.safetensors", "1"}}, h.installs)
	requireProgressCard(t, res, "job-h")
}

func TestHF_ApplyWithoutAPinnedCommitInstallsNothing(t *testing.T) {
	h := newHub()

	_, err := h.handler().Apply(context.Background(), project, op(t, install.OpInstallHFFile, install.HFArgs{Repo: "owner/style", Path: "style.safetensors"}))

	assert.Contains(t, refusal(t, err), "コミットが決まっていません")
	assert.Empty(t, h.installs)
}
