package installtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/install"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

func ask(t *testing.T, m *models, name string) (chatops.Card, error) {
	t.Helper()
	return install.NewModel(m.deps()).Question(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: name}))
}

func TestModel_AnIDAndANameBothResolveToTheCatalogEntryAndAskFirst(t *testing.T) {
	m := newModels(schnell, dev)
	for _, said := range []string{"flux-schnell", "FLUX.1 schnell", "flux1 schnell", "  Flux.1 SCHNELL "} {
		card, err := ask(t, m, said)

		require.NoError(t, err, said)
		require.NotNil(t, card.Op, "the yes carries the id the card resolved")
		var a install.ModelArgs
		require.NoError(t, card.Op.DecodeArgs(&a))
		assert.Equal(t, "flux-schnell", a.Model, said)
		assert.Empty(t, a.LicenseID, "a license that needs no acceptance carries none")
	}
	assert.Empty(t, m.installs, "asking installs nothing")
}

func TestModel_TheCardShowsSizeLicenseCommercialUseAndThatItAffectsEveryProject(t *testing.T) {
	m := newModels(schnell)

	card, err := ask(t, m, "flux-schnell")

	require.NoError(t, err)
	for _, want := range []string{"FLUX.1 schnell", "3.4 GB", "100.0 GB", "Apache License 2.0", "（apache-2.0 1）", "商用利用: 商用OK", "VRAM の目安: 12 GB", "全プロジェクト", "ディスク"} {
		assert.Contains(t, card.Text, want)
	}
	assert.Empty(t, card.Choices)
	assert.NotContains(t, card.Text, "同意して導入する", "no consent to ask for")
	assert.Empty(t, m.installs)
}

func TestModel_AModelWhoseLicenseNeedsConsentIsAskedWithTheLicenseAndOnlyItsButtonAccepts(t *testing.T) {
	m := newModels(schnell, dev)

	card, err := ask(t, m, "FLUX.1 dev")

	require.NoError(t, err)
	for _, want := range []string{"FLUX.1 [dev] Non-Commercial License", "flux-dev-nc", "版 2", "非商用のみ", "https://example.com/flux-dev", "条件つき（出力のみ商用可）", "同意して導入する", "全プロジェクト"} {
		assert.Contains(t, card.Text, want)
	}
	require.Len(t, card.Choices, 1)
	assert.Equal(t, "同意して導入する", card.Choices[0].Label)
	var a install.ModelArgs
	require.NoError(t, card.Choices[0].Op.DecodeArgs(&a))
	assert.Equal(t, install.ModelArgs{Model: "flux-dev", LicenseID: "flux-dev-nc", LicenseVersion: "2"}, a, "the button carries the license the card showed")
	b := body(t, op(t, install.OpInstallModel, install.ModelArgs{Model: "FLUX.1 dev"}), card)
	assert.Equal(t, []string{"同意して導入する", chatops.No}, b.Options, "no plain yes: a 「はい」 in words does not accept a license")
	assert.Empty(t, m.installs)
}

func TestModel_AnAlreadyAcceptedLicenseIsNotAskedAgain(t *testing.T) {
	accepted := dev
	accepted.Accepted = true
	m := newModels(accepted)

	card, err := ask(t, m, "flux-dev")

	require.NoError(t, err)
	assert.Empty(t, card.Choices)
	require.NotNil(t, card.Op)
	assert.NotContains(t, card.Text, "同意して導入する")
}

func TestModel_ANameThatMatchesSeveralModelsAsksWhichOneAndInstallsNone(t *testing.T) {
	m := newModels(schnell, dev, install.ModelInfo{ID: "sdxl", Name: "SDXL base", License: install.License{Commercial: "yes"}})

	card, err := ask(t, m, "flux")

	require.NoError(t, err)
	require.Len(t, card.Choices, 2)
	assert.Equal(t, []string{"FLUX.1 schnell", "FLUX.1 dev"}, []string{card.Choices[0].Label, card.Choices[1].Label})
	for i, id := range []string{"flux-schnell", "flux-dev"} {
		var a install.ModelArgs
		require.NoError(t, card.Choices[i].Op.DecodeArgs(&a))
		assert.Equal(t, install.ModelArgs{Model: id, Pick: true}, a, "a button carries the resolved id")
	}
	assert.Empty(t, m.installs)
}

func TestModel_PickingACandidateAsksTheConfirmationOfThatModelAndInstallsNothing(t *testing.T) {
	m := newModels(schnell, dev)
	h := install.NewModel(m.deps())

	res, err := h.Apply(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: "flux-dev", Pick: true}))

	require.NoError(t, err)
	assert.Empty(t, m.installs)
	require.Len(t, res.Messages, 1)
	assert.Contains(t, res.Messages[0].Content, "同意して導入する")
	assert.Equal(t, "operation", res.Messages[0].RefType)
}

func TestModel_ANameThatIsNotInTheCatalogIsRefusedAndNeverInstalled(t *testing.T) {
	m := newModels(schnell)

	_, err := ask(t, m, "totally-made-up-model")

	assert.Contains(t, refusal(t, err), "カタログに「totally-made-up-model」というモデルはありません")
	_, err = ask(t, m, "")
	assert.Contains(t, refusal(t, err), "どのモデルを導入しますか")
	_, err = install.NewModel(m.deps()).Apply(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: "totally-made-up-model"}))
	require.Error(t, err)
	assert.Empty(t, m.installs, "an id that is not in the catalog is never handed to the install")
}

func TestModel_AnAdultModelBeforeTheAgeConfirmationIsNeitherOfferedNorInstalled(t *testing.T) {
	m := newModels(schnell) // the catalog the gate lets through has no adult model
	m.hidden["adult-xl"] = modelmanager.ErrAdultNotConfirmed

	_, err := ask(t, m, "Adult XL")
	assert.Contains(t, refusal(t, err), "カタログに", "by name it is not found: it is not offered")
	_, err = ask(t, m, "adult-xl")
	assert.Contains(t, refusal(t, err), "年齢の確認", "by id the age gate answers as it does for the MCP tool")
	_, err = install.NewModel(m.deps()).Apply(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: "adult-xl"}))
	assert.Contains(t, refusal(t, err), "年齢の確認")
	assert.Empty(t, m.installs)
}

func TestModel_AnAdultModelAfterTheConfirmationShowsItsNotice(t *testing.T) {
	adult := adultModel
	adult.AdultNotice = "成人向けの内容を生成できるモデルです"
	m := newModels(adult)

	card, err := ask(t, m, "Adult XL")

	require.NoError(t, err)
	assert.Contains(t, card.Text, "成人向けの内容を生成できるモデルです")
	assert.Contains(t, card.Text, "商用利用: 不明")
}

func TestModel_ThingsThatRefuseTheInstallAreToldBeforeAnyCard(t *testing.T) {
	cases := map[string]struct {
		change func(*models)
		want   string
	}{
		"no engine yet":            {func(m *models) { m.env.EngineInstalled = false }, "先に ComfyUI エンジンを導入してください"},
		"a pinned settings file":   {func(m *models) { m.env.Pinned = true }, "--model-config"},
		"an OS without engine":     {func(m *models) { m.env.Supported = false }, "Windows 版と Apple Silicon"},
		"a folder it cannot write": {func(m *models) { m.env.Writable = false }, "書き込めません"},
		"no room":                  {func(m *models) { m.env.FreeBytes = uint64(gb) }, "空き容量が足りません（必要 3.4 GB、空き 1.0 GB）"},
		"no Hugging Face token": {func(m *models) {
			m.token = false
			m.catalog = []install.ModelInfo{withToken(schnell)}
		}, "Hugging Face のトークンが要ります"},
		"custom nodes missing": {func(m *models) {
			n := schnell
			n.NodeReason = "追加ノード「GGUF」を先に導入してください"
			m.catalog = []install.ModelInfo{n}
		}, "追加ノード「GGUF」"},
		"an install already running": {func(m *models) {
			n := schnell
			n.InstallJob = "job-9"
			m.catalog = []install.ModelInfo{n}
		}, "すでに導入中"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := newModels(schnell)
			c.change(m)

			card, err := ask(t, m, "flux-schnell")

			assert.Contains(t, refusal(t, err), c.want)
			assert.Empty(t, card.Text, "no card for something that is refused")
			assert.Empty(t, m.installs)
		})
	}
}

func withToken(m install.ModelInfo) install.ModelInfo {
	m.RequiresHFToken = true
	return m
}

func TestModel_AnAlreadyInstalledModelChangesNothing(t *testing.T) {
	done := schnell
	done.Installed = true
	m := newModels(done)

	_, err := ask(t, m, "flux-schnell")

	assert.ErrorIs(t, err, chatops.ErrNoChange)
	assert.Empty(t, m.installs)
}

func TestModel_ApplyQueuesTheInstallOnceAndAnswersWithAProgressCard(t *testing.T) {
	m := newModels(schnell)
	h := install.NewModel(m.deps())

	res, err := h.Apply(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: "flux-schnell"}))

	require.NoError(t, err)
	assert.Equal(t, []modelInstall{{ID: "flux-schnell"}}, m.installs, "exactly one install, without an acceptance")
	assert.Contains(t, res.Text, "「FLUX.1 schnell」の導入を始めました")
	requireProgressCard(t, res, "job-1")
	assert.Contains(t, res.Messages[0].Content, "FLUX.1 schnell")
}

func TestModel_ApplyCarriesTheAcceptedLicenseOfTheCardToTheInstall(t *testing.T) {
	m := newModels(dev)
	h := install.NewModel(m.deps())

	_, err := h.Apply(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: "flux-dev", LicenseID: "flux-dev-nc", LicenseVersion: "2"}))

	require.NoError(t, err)
	assert.Equal(t, []modelInstall{{ID: "flux-dev", Accept: &install.LicenseRef{ID: "flux-dev-nc", Version: "2"}}}, m.installs)
}

func TestModel_ApplyAnInstallThatWasAlreadyRunningSaysSoAndShowsThatJob(t *testing.T) {
	m := newModels(schnell)
	deps := m.deps()
	deps.Install = func(context.Context, string, *install.LicenseRef) (install.Job, error) {
		return install.Job{ID: "job-7", Existing: true}, nil
	}

	res, err := install.NewModel(deps).Apply(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: "flux-schnell"}))

	require.NoError(t, err)
	assert.Contains(t, res.Text, "すでに導入中")
	requireProgressCard(t, res, "job-7")
}

func TestModel_ApplyTellsTheRefusalsOfTheServiceAsTheMCPToolDoes(t *testing.T) {
	cases := map[error]string{
		modelmanager.ErrConsentRequired:    "ライセンスに同意してから導入してください",
		modelmanager.ErrPinned:             "--model-config",
		modelmanager.ErrEngineNotInstalled: "先に ComfyUI エンジンを導入してください",
		modelmanager.ErrAlreadyInstalled:   "すでに導入済み",
		modelmanager.ErrAdultNotConfirmed:  "年齢の確認",
	}
	for cause, want := range cases {
		m := newModels(schnell)
		m.installErr = cause

		res, err := install.NewModel(m.deps()).Apply(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: "flux-schnell"}))

		assert.Contains(t, refusal(t, err), want)
		assert.Empty(t, res.Messages, "no progress card for an install that did not start")
	}
	m := newModels(schnell)
	m.installErr = errors.New("disk on fire")
	_, err := install.NewModel(m.deps()).Apply(context.Background(), project, op(t, install.OpInstallModel, install.ModelArgs{Model: "flux-schnell"}))
	var r *chatops.Refusal
	assert.False(t, errors.As(err, &r), "a fault is not told as a refusal")
}
