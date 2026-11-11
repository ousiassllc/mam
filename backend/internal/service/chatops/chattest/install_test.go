package chattest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/install"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

const installGB = int64(1 << 30)

// installFake is the model manager of the install operation: what the chat handed to the install is in installs, the job that "runs" is job-1.
type installFake struct {
	catalog  []install.ModelInfo
	installs []string
	accepted []*install.LicenseRef
}

func (m *installFake) deps() install.ModelDeps {
	return install.ModelDeps{
		Catalog: func() []install.ModelInfo { return m.catalog },
		Model: func(id string) (install.ModelInfo, error) {
			for _, c := range m.catalog {
				if c.ID == id {
					return c, nil
				}
			}
			return install.ModelInfo{}, modelmanager.ErrNotFound
		},
		Environment: func(context.Context) (install.Environment, error) {
			return install.Environment{Supported: true, Writable: true, EngineInstalled: true, FreeBytes: uint64(100 * installGB)}, nil
		},
		HasHFToken: func() (bool, error) { return true, nil },
		Install: func(_ context.Context, id string, accept *install.LicenseRef) (install.Job, error) {
			m.installs, m.accepted = append(m.installs, id), append(m.accepted, accept)
			return install.Job{ID: "job-1"}, nil
		},
	}
}

func newInstallFixture(t *testing.T, models ...install.ModelInfo) (*chatfix.Fixture, *installFake) {
	t.Helper()
	f := chatfix.New(t)
	fake := &installFake{catalog: models}
	f.Svc.ChatOps().Handle(install.OpInstallModel, install.NewModel(fake.deps()))
	f.Svc.ChatOps().Progress(install.Detail(func(_ context.Context, id string) (install.JobProgress, bool) {
		if id != "job-1" {
			return install.JobProgress{}, false
		}
		return install.JobProgress{State: domain.JobRunning, Percent: 42, Phase: "download", DoneBytes: installGB, TotalBytes: 3 * installGB}, true
	}))
	f.Direct()
	return f, fake
}

var (
	installSchnell = install.ModelInfo{
		ID: "flux-schnell", Name: "FLUX.1 schnell", TotalBytes: 3 * installGB, NeedBytes: 3 * installGB,
		License: install.License{ID: "apache-2.0", Version: "1", Name: "Apache License 2.0", Commercial: "yes"},
	}
	installDev = install.ModelInfo{
		ID: "flux-dev", Name: "FLUX.1 dev", TotalBytes: 20 * installGB, NeedBytes: 20 * installGB, ConsentRequired: true,
		License: install.License{ID: "flux-dev-nc", Version: "2", Name: "FLUX.1 [dev] Non-Commercial License", URL: "https://example.com/flux-dev", Commercial: "conditional", Condition: "出力のみ商用可"},
	}
)

func sayInstall(f *chatfix.Fixture, model, message string) service.Workspace {
	f.T.Helper()
	return f.SayOps(message, `{"type":"install_model","args":{"model":"`+model+`"}}`)
}

// "FLUX.1 schnell を入れて": the card first (name, size, license, commercial use, that it is global), nothing is queued; declining or an unrelated reply queues nothing.
func TestChat_InstallAModelAsksFirstAndDecliningQueuesNothing(t *testing.T) {
	f, fake := newInstallFixture(t, installSchnell, installDev)

	ws := sayInstall(f, "FLUX.1 schnell", "FLUX.1 schnell のモデルを入れて")

	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1)
	assert.Equal(t, "install_model", cards[0].RefID)
	body := chatfix.Body(cards[0])
	for _, want := range []string{"FLUX.1 schnell", "3.0 GB", "Apache License 2.0", "商用OK", "全プロジェクト"} {
		assert.Contains(t, body.Text, want)
	}
	assert.Equal(t, []string{"はい、この内容で進める", "やめる"}, body.Options)
	assert.Empty(t, fake.installs, "nothing is queued before the user accepts")

	f.LLM.Interpreter = chatfix.OpsJSON()
	f.Say("カメラを固定して", cards[0].ID)
	assert.Empty(t, fake.installs, "an unrelated reply does not accept")

	card := chatfix.Cards(sayInstall(f, "FLUX.1 schnell", "FLUX.1 schnell のモデルを入れて").Messages)[0]
	ws = f.Answer(card.ID, "やめる")
	assert.Equal(t, "やめました。何も変更していません。", chatfix.LastText(ws))
	assert.Empty(t, fake.installs)
}

// Accepting queues the install once and the thread gets a progress card of that job, with how far it is.
func TestChat_AcceptingTheInstallQueuesItOnceAndPostsAProgressCard(t *testing.T) {
	f, fake := newInstallFixture(t, installSchnell)
	card := chatfix.Cards(sayInstall(f, "FLUX.1 schnell", "FLUX.1 schnell を導入して").Messages)[0]

	ws := f.Answer(card.ID, "はい、この内容で進める")

	assert.Equal(t, []string{"flux-schnell"}, fake.installs)
	assert.Equal(t, []*install.LicenseRef{nil}, fake.accepted)
	var progress []domain.ThreadMessage
	for _, m := range f.Thread() {
		if m.Kind == domain.KindProgress {
			progress = append(progress, m)
		}
	}
	require.Len(t, progress, 1)
	assert.Equal(t, domain.RefJob, progress[0].RefType)
	assert.Equal(t, "job-1", progress[0].RefID)
	assert.Contains(t, progress[0].Content, "FLUX.1 schnell")
	assert.Equal(t, "42% ・ダウンロード中（1.0 / 3.0 GB）", progress[0].Detail)
	assert.Contains(t, ws.Messages[len(ws.Messages)-2].Content, "導入を始めました")
	_, err := f.Svc.Chat.PostMessage(context.Background(), f.ProjectID, service.PostMessageInput{Content: "はい", ReplyToMessageID: card.ID})
	assert.ErrorIs(t, err, service.ErrConflict, "a card is answered once")
}

// A license that needs consent: the card shows its ID and version, only the button 「同意して導入する」 accepts, and it carries the license to the install.
func TestChat_AConsentLicenseIsAcceptedOnlyByItsButton(t *testing.T) {
	f, fake := newInstallFixture(t, installSchnell, installDev)
	card := chatfix.Cards(sayInstall(f, "flux-dev", "FLUX.1 dev を導入して").Messages)[0]
	body := chatfix.Body(card)
	for _, want := range []string{"flux-dev-nc", "版 2", "https://example.com/flux-dev", "同意して導入する", "全プロジェクト"} {
		assert.Contains(t, body.Text, want)
	}
	require.Equal(t, []string{"同意して導入する", "やめる"}, body.Options)

	f.LLM.Interpreter = chatfix.OpsJSON()
	f.Say("はい", card.ID)
	assert.Empty(t, fake.installs, "a yes in words is not the acceptance of a license")

	card = chatfix.Cards(sayInstall(f, "flux-dev", "FLUX.1 dev を導入して").Messages)[0]
	f.Answer(card.ID, "同意して導入する")

	assert.Equal(t, []string{"flux-dev"}, fake.installs)
	assert.Equal(t, []*install.LicenseRef{{ID: "flux-dev-nc", Version: "2"}}, fake.accepted)
}

// A name that fits several models asks which; the pick asks the confirmation of that model, and only its yes installs.
func TestChat_AnAmbiguousModelNameAsksWhichAndThenConfirms(t *testing.T) {
	f, fake := newInstallFixture(t, installSchnell, installDev)
	ws := sayInstall(f, "flux", "flux のモデルを入れて")
	card := chatfix.Cards(ws.Messages)[0]
	body := chatfix.Body(card)
	require.Equal(t, []string{"FLUX.1 schnell", "FLUX.1 dev", "やめる"}, body.Options)
	assert.Empty(t, fake.installs)

	ws = f.Answer(card.ID, "FLUX.1 schnell")

	assert.Empty(t, fake.installs, "picking a candidate installs nothing")
	cards := chatfix.Cards(ws.Messages)
	require.Len(t, cards, 1, "the confirmation card of the model picked")
	assert.Contains(t, chatfix.Body(cards[0]).Text, "Apache License 2.0")
	f.Answer(cards[0].ID, "はい、この内容で進める")
	assert.Equal(t, []string{"flux-schnell"}, fake.installs)
}

// A model that is not in the catalog never reaches the install; one that is installed already is "nothing to do"; the model's word alone does not install.
func TestChat_AnInstallOfWhatTheCatalogDoesNotHaveOrOfWhatIsThereOrWasNotAskedForIsNotCarriedOut(t *testing.T) {
	done := installSchnell
	done.Installed = true
	f, fake := newInstallFixture(t, done)

	ws := sayInstall(f, "model-that-does-not-exist", "model-that-does-not-exist のモデルを入れて")
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "カタログに「model-that-does-not-exist」というモデルはありません")

	ws = sayInstall(f, "flux-schnell", "FLUX.1 schnell のモデルを入れて")
	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "すでにその内容です")

	ws = sayInstall(f, "flux-schnell", "カメラを固定して")
	assert.Empty(t, chatfix.Cards(ws.Messages), "the model's operation for a message that does not ask for an install is dropped")
	assert.Empty(t, fake.installs)
}

// An install the app has no model manager for says so instead of failing.
func TestChat_AnInstallWithoutAModelManagerSaysItIsNotAvailable(t *testing.T) {
	f := chatfix.New(t)
	f.Direct()

	ws := sayInstall(f, "flux-schnell", "FLUX.1 schnell のモデルを入れて")

	assert.Empty(t, chatfix.Cards(ws.Messages))
	assert.Contains(t, chatfix.LastText(ws), "この環境では、その設定をチャットから変更できません")
}
