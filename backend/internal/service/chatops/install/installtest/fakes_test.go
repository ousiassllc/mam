// Package installtest tests the chat operations of the installs (package install; spec/components/chat-settings-operations.md §2) with fakes of the model manager, the Hugging
// Face repositories and the local runtimes. It is a package of its own because the directory of the operation is at its line budget.
package installtest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/install"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
)

const gb = int64(1 << 30)

// home is an environment where an install can start: the engine is there and there is room.
var home = install.Environment{Supported: true, Writable: true, EngineInstalled: true, FreeBytes: uint64(100 * gb)}

var (
	schnell = install.ModelInfo{
		ID: "flux-schnell", Name: "FLUX.1 schnell", Kind: "image", TotalBytes: 34 * gb / 10, NeedBytes: 34 * gb / 10, VRAMMinGB: 12,
		License: install.License{ID: "apache-2.0", Version: "1", Name: "Apache License 2.0", URL: "https://example.com/apache", Commercial: "yes"},
	}
	dev = install.ModelInfo{
		ID: "flux-dev", Name: "FLUX.1 dev", Kind: "image", TotalBytes: 23 * gb, NeedBytes: 23 * gb, ConsentRequired: true,
		License: install.License{
			ID: "flux-dev-nc", Version: "2", Name: "FLUX.1 [dev] Non-Commercial License", URL: "https://example.com/flux-dev", Summary: "非商用のみ。出力の商用利用は別の条件。",
			Commercial: "conditional", Condition: "出力のみ商用可", Notice: "商用で使う前に原文を読んでください",
		},
	}
	adultModel = install.ModelInfo{ID: "adult-xl", Name: "Adult XL", Adult: true, TotalBytes: 7 * gb, NeedBytes: 7 * gb, License: install.License{Commercial: "unknown"}}
)

// models is the fake of the model manager the model handler is given: what it was asked to install is in installs.
type models struct {
	catalog    []install.ModelInfo
	hidden     map[string]error // models Model refuses (an adult model before the age confirmation)
	env        install.Environment
	token      bool
	installErr error
	installs   []modelInstall
}

type modelInstall struct {
	ID     string
	Accept *install.LicenseRef
}

func newModels(list ...install.ModelInfo) *models {
	return &models{catalog: list, env: home, token: true, hidden: map[string]error{}}
}

func (m *models) deps() install.ModelDeps {
	return install.ModelDeps{
		Catalog: func() []install.ModelInfo { return m.catalog },
		Model: func(id string) (install.ModelInfo, error) {
			if err, ok := m.hidden[id]; ok {
				return install.ModelInfo{}, err
			}
			for _, c := range m.catalog {
				if c.ID == id {
					return c, nil
				}
			}
			return install.ModelInfo{}, modelmanager.ErrNotFound
		},
		Environment: func(context.Context) (install.Environment, error) { return m.env, nil },
		HasHFToken:  func() (bool, error) { return m.token, nil },
		Install: func(_ context.Context, id string, accept *install.LicenseRef) (install.Job, error) {
			m.installs = append(m.installs, modelInstall{ID: id, Accept: accept})
			return install.Job{ID: "job-1"}, m.installErr
		},
	}
}

var project = domain.Project{ID: "p1"}

func op(t *testing.T, kind commandinterpreter.OpType, args any) commandinterpreter.Operation {
	t.Helper()
	o, err := commandinterpreter.NewOperation(kind, args)
	require.NoError(t, err)
	return o
}

// refusal is the sentence of the refusal err is.
func refusal(t *testing.T, err error) string {
	t.Helper()
	var r *chatops.Refusal
	require.ErrorAs(t, err, &r, "a refusal the user can act on")
	return r.Message
}

// body is the card as the chat posts it.
func body(t *testing.T, o commandinterpreter.Operation, card chatops.Card) domain.CardBody {
	t.Helper()
	b, err := chatops.Body(o, card)
	require.NoError(t, err)
	return b
}

// yesOp is the operation the yes button of a card carries.
func yesOp(t *testing.T, o commandinterpreter.Operation, card chatops.Card) commandinterpreter.Operation {
	t.Helper()
	b := body(t, o, card)
	require.Equal(t, chatops.Yes, b.Options[0], "a card with a yes button")
	var out commandinterpreter.Operation
	require.NoError(t, json.Unmarshal(b.OptionActions[0], &out))
	return out
}

func requireProgressCard(t *testing.T, res chatops.Result, jobID string) {
	t.Helper()
	require.Len(t, res.Messages, 1)
	m := res.Messages[0]
	assert.Equal(t, domain.KindProgress, m.Kind)
	assert.Equal(t, domain.RefJob, m.RefType)
	assert.Equal(t, jobID, m.RefID)
	assert.Equal(t, domain.RoleAssistant, m.Role)
	assert.Contains(t, m.Content, "を導入しています")
}
