// Package chatfix builds the services for the tests of the chat operations of the global settings (#1140; service/chatops/chattest): a database, the services over it with
// a scripted Command Interpreter, one Project, and helpers to talk to its chat and read the cards. It is for tests only. The tests of the chat operations are not in package
// service because that directory is at its size limit.
package chatfix

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/credential/credentialtest"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
	"github.com/ousiassllc/mam/backend/internal/service/servicetest"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

// LLM is the Reasoning LLM of the fixture: it answers the Command Interpreter from Interpreter (no operation when nil) and every other Agent with an empty JSON object. It
// keeps what each Agent was asked.
type LLM struct {
	mu          sync.Mutex
	Interpreter func(user string) string
	asked       map[llm.Agent][]string
}

// Complete implements llm.Completer.
func (l *LLM) Complete(_ context.Context, agent llm.Agent, req llm.Request) (llm.Response, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.asked == nil {
		l.asked = map[llm.Agent][]string{}
	}
	l.asked[agent] = append(l.asked[agent], req.User)
	text := `{}`
	if agent == llm.AgentCommandInterpreter {
		text = `{"operations":[]}`
		if l.Interpreter != nil {
			text = l.Interpreter(req.User)
		}
	}
	return llm.Response{Text: text, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

// Asked is a copy of the prompts the Agent received.
func (l *LLM) Asked(agent llm.Agent) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.asked[agent]...)
}

// OpsJSON is an Interpreter that answers every message with these operations (JSON objects).
func OpsJSON(ops ...string) func(string) string {
	return func(string) string { return `{"operations":[` + strings.Join(ops, ",") + `]}` }
}

// Fixture is the services, one Project and the means to talk to it.
type Fixture struct {
	T         *testing.T
	Svc       *service.Services
	DB        *repository.DB
	AssetsDir string
	ProjectID string
	LLM       *LLM
	Keys      *credentialtest.Memory
	// Logs is everything the services logged (debug level).
	Logs *bytes.Buffer
}

// Option changes how New builds the fixture.
type Option func(*options)

type options struct {
	tweak    func(*service.Deps)
	settings *chatwire.Settings
	noLLM    bool
}

// WithDeps changes the service.Deps before the services are built.
func WithDeps(tweak func(*service.Deps)) Option { return func(o *options) { o.tweak = tweak } }

// Wired connects the chat operations to the services (chatwire.Connect) with these global settings services; without it the test adds the handlers it needs.
func Wired(s chatwire.Settings) Option { return func(o *options) { o.settings = &s } }

// WithoutLLM builds the services without a Reasoning LLM: the case where the key of the LLM itself is what the user is setting.
func WithoutLLM() Option { return func(o *options) { o.noLLM = true } }

// New builds the services over a database of its own and creates a Project in DRAFT (AUTO MODE). The test runs in parallel with the others.
func New(t *testing.T, opts ...Option) *Fixture {
	t.Helper()
	testenv.Parallel(t)
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	f := &Fixture{T: t, DB: testdb.Open(t), LLM: &LLM{}, Keys: credentialtest.New(), Logs: &bytes.Buffer{}}
	f.AssetsDir = filepath.Join(t.TempDir(), "assets")
	deps := service.Deps{DB: f.DB, AssetsDir: f.AssetsDir, Credentials: f.Keys, Logger: slog.New(slog.NewTextHandler(f.Logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	if !o.noLLM {
		deps.LLM = f.LLM
	}
	if o.tweak != nil {
		o.tweak(&deps)
	}
	f.Svc = servicetest.New(t, deps)
	if o.settings != nil {
		chatwire.Connect(f.Svc, *o.settings)
	}
	p, err := f.Svc.Projects.Create(context.Background(), service.CreateProjectInput{Content: "美容液のCMを作りたい"})
	require.NoError(t, err)
	f.ProjectID = p.ID
	return f
}

// SQL runs a statement on the database of the fixture.
func (f *Fixture) SQL(q string, args ...any) {
	f.T.Helper()
	require.NoError(f.T, f.DB.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}))
}

// Direct puts the Project in DIRECTOR MODE after its Brief (the state in which the Command Interpreter reads every message).
func (f *Fixture) Direct() {
	f.T.Helper()
	f.SQL(`UPDATE project SET mode = 'director', state = 'GENERATING' WHERE id = ?`, f.ProjectID)
}

// Auto puts the Project in AUTO MODE past the Brief.
func (f *Fixture) Auto() {
	f.T.Helper()
	f.SQL(`UPDATE project SET state = 'PLANNING' WHERE id = ?`, f.ProjectID)
}

// Post sends a message with these uploads, as a reply to replyTo when it is not empty.
func (f *Fixture) Post(in service.PostMessageInput) service.Workspace {
	f.T.Helper()
	ws, err := f.Svc.Chat.PostMessage(context.Background(), f.ProjectID, in)
	require.NoError(f.T, err)
	return ws
}

// Say sends a message of the user.
func (f *Fixture) Say(content, replyTo string) service.Workspace {
	f.T.Helper()
	return f.Post(service.PostMessageInput{Content: content, ReplyToMessageID: replyTo})
}

// SayOps scripts the Command Interpreter to return these operations (JSON objects) and sends the message.
func (f *Fixture) SayOps(message string, ops ...string) service.Workspace {
	f.T.Helper()
	f.LLM.Interpreter = OpsJSON(ops...)
	return f.Say(message, "")
}

// Answer replies to a card.
func (f *Fixture) Answer(cardID, reply string) service.Workspace {
	f.T.Helper()
	return f.Say(reply, cardID)
}

// Thread is the whole thread of the Project.
func (f *Fixture) Thread() []domain.ThreadMessage {
	f.T.Helper()
	ws, err := f.Svc.Chat.Workspace(context.Background(), f.ProjectID)
	require.NoError(f.T, err)
	return ws.Messages
}

// Cards are the confirmation cards (ref_type operation) among the messages.
func Cards(msgs []domain.ThreadMessage) []domain.ThreadMessage {
	var cards []domain.ThreadMessage
	for _, m := range msgs {
		if m.Kind == domain.KindQuestion && m.RefType == domain.RefOperation {
			cards = append(cards, m)
		}
	}
	return cards
}

// LastText is the content of the last message of the answer.
func LastText(ws service.Workspace) string { return ws.Messages[len(ws.Messages)-1].Content }

// Body is the decoded body of a card.
func Body(card domain.ThreadMessage) domain.CardBody { return domain.DecodeCardBody(card.Content) }
