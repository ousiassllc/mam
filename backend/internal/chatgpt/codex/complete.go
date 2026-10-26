package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/rpc"
)

// Request is one text generation: a system prompt, a user message and, for the Agents that judge pictures, pictures.
type Request struct {
	Model  string
	System string
	User   string
	Images []Image
	// JSON asks for one JSON object as the answer. It is asked in words (there is no schema in a request); the
	// caller validates the answer.
	JSON bool
}

// Image is a picture of a Request. Caption, when set, is given as text just before the picture.
type Image struct {
	MimeType string
	Data     []byte
	Caption  string
}

const (
	jsonDemand = "\n\nAnswer with one JSON object and nothing else: no code fence, no text before or after it."
	// defaultSystem is sent when the Agent has no system prompt: with none, Codex's own coding-agent instructions would apply.
	defaultSystem = "You are a helpful assistant."
	// turnCleanupTimeout bounds the best-effort calls that end a thread or a turn.
	turnCleanupTimeout = 5 * time.Second
	// resetLookupTimeout bounds the read of the usage limit after UsageLimitExceeded.
	resetLookupTimeout = 10 * time.Second
)

// Complete generates text in one thread and one turn of the app-server: an ephemeral thread (no history on disk) in an
// empty working directory, in the read-only sandbox, with no approval ever given. It never uses a tool: every request of
// the server is refused, and a turn that starts a tool item is interrupted and fails. The answer is the final agent
// message. A failure is a *Failure (or the error of ctx).
func (s *Service) Complete(ctx context.Context, req Request) (string, error) {
	se, err := s.session(ctx)
	if err != nil {
		return "", startFailure(err)
	}
	if err := s.requireSignedIn(ctx, se); err != nil {
		return "", err
	}
	if len(req.Images) > 0 {
		if err := s.requireImageModel(ctx, req.Model); err != nil {
			return "", err
		}
	}
	paths, cleanup, err := s.writeImages(req.Images)
	if err != nil {
		return "", &Failure{Kind: KindPermanent, Message: "画像を一時ファイルに書き出せませんでした", Err: err}
	}
	defer cleanup()

	var th protocol.ThreadStartResult
	if err := se.Call(ctx, protocol.MethodThreadStart, s.threadParams(req), &th); err != nil {
		return "", callFailure(ctx, "thread/start", err)
	}
	threadID := th.Thread.ID
	sub := se.Subscribe(threadID)
	defer func() {
		se.Unsubscribe(threadID)
		bg, cancel := context.WithTimeout(context.Background(), turnCleanupTimeout)
		defer cancel()
		_ = se.Call(bg, protocol.MethodThreadUnsubscribe, protocol.ThreadRef{ThreadID: threadID}, nil)
	}()

	var turn protocol.TurnStartResult
	if err := se.Call(ctx, protocol.MethodTurnStart, turnParams(threadID, req, paths), &turn); err != nil {
		return "", callFailure(ctx, "turn/start", err)
	}
	return s.await(ctx, se, sub, threadID, turn.Turn.ID)
}

// startFailure maps a failure of the app-server start.
func startFailure(err error) error {
	if errors.Is(err, ErrEngineNotInstalled) {
		return &Failure{Kind: KindEngine, Message: "Codex のエンジンが導入されていません", Err: ErrEngineNotInstalled}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &Failure{Kind: KindTransient, Message: sanitize(err.Error()), Err: err}
}

// callFailure maps the failure of a request: the end of ctx is itself; an error response of the app-server to a request
// mam built is not worth another try; the connection ending, or anything else, is.
func callFailure(ctx context.Context, what string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	kind := KindTransient
	var rerr *rpc.Error
	if errors.As(err, &rerr) {
		kind = KindPermanent
	}
	return &Failure{Kind: kind, Message: what + ": " + sanitize(err.Error()), Err: err}
}

// requireSignedIn asks the app-server (account/read) whether there is a login: without one, the call goes straight to
// the alternative provider with a request to sign in.
func (s *Service) requireSignedIn(ctx context.Context, se *appserver.Session) error {
	var acc protocol.GetAccountResult
	if err := se.Call(ctx, protocol.MethodAccountRead, protocol.GetAccountParams{RefreshToken: false}, &acc); err != nil {
		return callFailure(ctx, "account/read", err)
	}
	if acc.Account == nil {
		s.setAccount(AccountView{SignedIn: false})
		return &Failure{Kind: KindNotSignedIn, Message: "Codex 経由でログインしていません"}
	}
	return nil
}

// requireImageModel refuses a picture for a model that does not take pictures. A model the list does not show is let
// through: the app-server says if it cannot take them.
func (s *Service) requireImageModel(ctx context.Context, model string) error {
	models, err := s.Models(ctx)
	if err != nil {
		return nil // the list is a check, not a requirement
	}
	for _, m := range models {
		if m.Slug == model && !m.Images {
			return &Failure{Kind: KindUnsupported, Message: fmt.Sprintf("モデル %s は画像の入力に対応していません", model)}
		}
	}
	return nil
}

func (s *Service) threadParams(req Request) protocol.ThreadStartParams {
	system := req.System
	if system == "" {
		system = defaultSystem
	}
	if req.JSON {
		system += jsonDemand
	}
	p := protocol.ThreadStartParams{
		Model: req.Model, Ephemeral: true, Cwd: s.work, Sandbox: protocol.SandboxReadOnly, ApprovalPolicy: protocol.ApprovalNever,
	}
	if s.opts.Instructions == InstructionsDeveloper {
		p.DeveloperInstructions = &system
	} else {
		p.BaseInstructions = &system
	}
	return p
}

func turnParams(threadID string, req Request, imagePaths []string) protocol.TurnStartParams {
	input := []any{protocol.TextInput{Type: protocol.InputText, Text: req.User}}
	for i, img := range req.Images {
		if img.Caption != "" {
			input = append(input, protocol.TextInput{Type: protocol.InputText, Text: img.Caption})
		}
		input = append(input, protocol.LocalImageInput{Type: protocol.InputLocalImage, Path: imagePaths[i]})
	}
	return protocol.TurnStartParams{
		ThreadID: threadID, Input: input, ApprovalPolicy: protocol.ApprovalNever,
		SandboxPolicy: &protocol.SandboxPolicy{Type: protocol.PolicyReadOnly},
	}
}

var imageExt = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}

// writeImages writes the pictures to a temporary directory below the working directory (the turn gets them as local
// files) and returns their paths and the function that removes them.
func (s *Service) writeImages(images []Image) (paths []string, cleanup func(), err error) {
	if len(images) == 0 {
		return nil, func() {}, nil
	}
	dir := filepath.Join(s.work, "img-"+uuid.NewString())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	for i, img := range images {
		ext, ok := imageExt[strings.ToLower(img.MimeType)]
		if !ok {
			cleanup()
			return nil, nil, fmt.Errorf("unsupported picture type %q", img.MimeType)
		}
		p := filepath.Join(dir, fmt.Sprintf("image-%d%s", i+1, ext))
		if err := os.WriteFile(p, img.Data, 0o600); err != nil {
			cleanup()
			return nil, nil, err
		}
		paths = append(paths, p)
	}
	return paths, cleanup, nil
}

// interrupt asks the app-server to stop the turn (best effort, on a context of its own: the caller's may be over).
func (s *Service) interrupt(se *appserver.Session, threadID, turnID string) {
	ctx, cancel := context.WithTimeout(context.Background(), turnCleanupTimeout)
	defer cancel()
	_ = se.Call(ctx, protocol.MethodTurnInterrupt, protocol.TurnInterruptParams{ThreadID: threadID, TurnID: turnID}, nil)
}

// endMessage says that the process ended during the turn, and how.
func endMessage(se *appserver.Session) string {
	if r := se.EndReason(); r != "" {
		return r
	}
	return "Codex app-server が応答の途中で終了しました"
}
