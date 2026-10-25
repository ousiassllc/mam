package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

func complete(r *rig, req codex.Request) (string, error) {
	if req.Model == "" {
		req.Model = "gpt-text"
	}
	return r.svc.Complete(context.Background(), req)
}

func failureOf(t *testing.T, err error) *codex.Failure {
	t.Helper()
	var f *codex.Failure
	if !errors.As(err, &f) {
		t.Fatalf("err = %v, want a *codex.Failure", err)
	}
	return f
}

func TestCompleteRunsOneEphemeralReadOnlyTurnAndReturnsTheFinalAnswer(t *testing.T) {
	r := newRig(t)
	r.fake.Script = func(turn *codextest.Turn) {
		turn.Item(map[string]any{"type": "agentMessage", "id": "a", "text": "考えています", "phase": "commentary"})
		turn.Delta("最終")
		turn.Final("最終の答え")
		turn.Complete(protocol.TurnCompleted, nil)
	}
	got, err := complete(r, codex.Request{System: "あなたは脚本家です", User: "脚本を書いて"})
	if err != nil || got != "最終の答え" {
		t.Fatalf("got %q, %v", got, err)
	}

	threads := r.fake.Threads()
	if len(threads) != 1 {
		t.Fatalf("threads = %d", len(threads))
	}
	tp := threads[0].Params
	if tp.Model != "gpt-text" || !tp.Ephemeral || tp.Sandbox != "read-only" || tp.ApprovalPolicy != "never" {
		t.Errorf("thread/start = %+v", tp)
	}
	if tp.BaseInstructions == nil || *tp.BaseInstructions != "あなたは脚本家です" || tp.DeveloperInstructions != nil {
		t.Errorf("the system prompt must go in baseInstructions (decision 8): %+v", tp)
	}
	if tp.Cwd != filepath.Join(r.dataDir, "codex-work") {
		t.Errorf("cwd = %q, want mam's empty working directory", tp.Cwd)
	}
	var turn protocol.TurnStartParams
	call := r.fake.CallsOf(protocol.MethodTurnStart)[0]
	if err := json.Unmarshal(call.Params, &turn); err != nil {
		t.Fatal(err)
	}
	if turn.ApprovalPolicy != "never" || turn.SandboxPolicy == nil || turn.SandboxPolicy.Type != "readOnly" || turn.ThreadID != threads[0].ThreadID {
		t.Errorf("turn/start = %+v", turn)
	}
	if !contains(string(call.Params), `"type":"text"`, "脚本を書いて") {
		t.Errorf("input = %s", call.Params)
	}
	if n := len(r.fake.CallsOf(protocol.MethodThreadUnsubscribe)); n != 1 {
		t.Errorf("thread/unsubscribe calls = %d", n)
	}
	acc := r.fake.CallsOf(protocol.MethodAccountRead)
	if len(acc) == 0 || !contains(string(acc[0].Params), `"refreshToken":false`) {
		t.Errorf("account/read must be asked with refreshToken false: %v", acc)
	}
}

func TestInstructionsGoInDeveloperInstructionsWhenSwitched(t *testing.T) {
	r := newRig(t, func(o *codex.Options) { o.Instructions = codex.InstructionsDeveloper })
	if _, err := complete(r, codex.Request{System: "S", User: "u", JSON: true}); err != nil {
		t.Fatal(err)
	}
	tp := r.fake.Threads()[0].Params
	if tp.BaseInstructions != nil || tp.DeveloperInstructions == nil || !strings.HasPrefix(*tp.DeveloperInstructions, "S") ||
		!strings.Contains(*tp.DeveloperInstructions, "one JSON object") {
		t.Errorf("thread/start = %+v", tp)
	}
	if codex.DefaultInstructions != codex.InstructionsBase {
		t.Error("baseInstructions is the first candidate until a comparison on a real machine says otherwise")
	}
}

func TestAnEmptySystemPromptGetsADefaultSoCodexsCodingInstructionsDoNotApply(t *testing.T) {
	r := newRig(t)
	if _, err := complete(r, codex.Request{User: "u"}); err != nil {
		t.Fatal(err)
	}
	if bi := r.fake.Threads()[0].Params.BaseInstructions; bi == nil || *bi == "" {
		t.Error("baseInstructions must be set")
	}
}

func TestTheAnswerFallsBackToTheDeltasWhenNoFinalMessageIsMarked(t *testing.T) {
	r := newRig(t)
	r.fake.Script = func(turn *codextest.Turn) {
		turn.Delta("こん")
		turn.Delta("にちは")
		turn.Complete(protocol.TurnCompleted, nil)
	}
	if got, err := complete(r, codex.Request{User: "u"}); err != nil || got != "こんにちは" {
		t.Fatalf("got %q, %v", got, err)
	}
	r.fake.Script = func(turn *codextest.Turn) { turn.Complete(protocol.TurnCompleted, nil) }
	if f := failureOf(t, errOf(complete(r, codex.Request{User: "u"}))); f.Kind != codex.KindTransient {
		t.Errorf("an empty answer: %+v", f)
	}
}

func errOf(_ string, err error) error { return err }

func TestPicturesGoAsLocalFilesAndAreRemovedAfterTheTurn(t *testing.T) {
	r := newRig(t)
	var seen []string
	r.fake.Script = func(turn *codextest.Turn) {
		var tp protocol.TurnStartParams
		_ = json.Unmarshal(turn.Params, &tp)
		for _, in := range tp.Input {
			if m, ok := in.(map[string]any); ok && m["type"] == "localImage" {
				p, _ := m["path"].(string)
				if b, err := os.ReadFile(p); err == nil {
					seen = append(seen, string(b))
				}
			}
		}
		codextest.Say("ok")(turn)
	}
	images := []codex.Image{{MimeType: "image/png", Data: []byte("PNG-1"), Caption: "frame 1"}, {MimeType: "image/jpeg", Data: []byte("JPG-2")}}
	if _, err := r.svc.Complete(context.Background(), codex.Request{Model: "gpt-vision", User: "judge", Images: images}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "PNG-1" || seen[1] != "JPG-2" {
		t.Fatalf("the model must see the pictures as local files while the turn runs: %v", seen)
	}
	raw := string(r.fake.CallsOf(protocol.MethodTurnStart)[0].Params)
	if !contains(raw, `"text":"frame 1"`, `"type":"localImage"`, ".png", ".jpg") {
		t.Errorf("input = %s", raw)
	}
	entries, _ := os.ReadDir(filepath.Join(r.dataDir, "codex-work"))
	if len(entries) != 0 {
		t.Errorf("temporary pictures are left behind: %v", entries)
	}
}

func TestAPictureIsNotSentToAModelThatTakesNone(t *testing.T) {
	r := newRig(t)
	_, err := r.svc.Complete(context.Background(), codex.Request{Model: "gpt-text", User: "u", Images: []codex.Image{{MimeType: "image/png", Data: []byte("x")}}})
	if f := failureOf(t, err); f.Kind != codex.KindUnsupported {
		t.Fatalf("failure = %+v", f)
	}
	if len(r.fake.Threads()) != 0 {
		t.Error("nothing may be sent to the model")
	}
	// A model the list does not show is let through: the app-server decides.
	if _, err := r.svc.Complete(context.Background(), codex.Request{Model: "gpt-unlisted", User: "u", Images: []codex.Image{{MimeType: "image/png", Data: []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	// An unsupported picture type fails before anything is sent.
	if _, err := r.svc.Complete(context.Background(), codex.Request{Model: "gpt-vision", User: "u", Images: []codex.Image{{MimeType: "image/tiff", Data: []byte("x")}}}); err == nil {
		t.Error("a picture type Codex cannot read must fail")
	}
}

func TestNotSignedInGoesToTheAlternativeWithoutStartingAThread(t *testing.T) {
	r := newRig(t)
	r.fake.Account = nil
	f := failureOf(t, errOf(complete(r, codex.Request{User: "u"})))
	if f.Kind != codex.KindNotSignedIn {
		t.Fatalf("failure = %+v", f)
	}
	if len(r.fake.Threads()) != 0 {
		t.Error("no thread may be started without a login")
	}
	if r.svc.Status().Account.SignedIn {
		t.Error("the cached account must follow")
	}
}

func TestWithoutAnEngineTheFailureSaysSo(t *testing.T) {
	r := newRig(t)
	if err := r.svc.SetCustomPath(""); err != nil {
		t.Fatal(err)
	}
	f := failureOf(t, errOf(complete(r, codex.Request{User: "u"})))
	if f.Kind != codex.KindEngine || !errors.Is(f, codex.ErrEngineNotInstalled) {
		t.Fatalf("failure = %+v", f)
	}
	if r.fake.Starts() != 0 {
		t.Error("nothing to start")
	}
}

func TestConcurrentCallsShareOneProcess(t *testing.T) {
	r := newRig(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := complete(r, codex.Request{User: "u"}); err != nil || got != "OK" {
				t.Errorf("got %q, %v", got, err)
			}
		}()
	}
	wg.Wait()
	if r.fake.Starts() != 1 || len(r.fake.Threads()) != 8 {
		t.Errorf("starts = %d, threads = %d", r.fake.Starts(), len(r.fake.Threads()))
	}
}
