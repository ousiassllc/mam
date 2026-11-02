package toolstatus

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/chatgpt"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/web/molecules"
	"github.com/ousiassllc/mam/backend/internal/web/paths"
)

// Services are what the cards read. A nil one leaves its cards without a status.
type Services struct {
	ModelManager *modelmanager.Service
	Credentials  *service.CredentialService
	// ChatGPT is optional: without it the settings status has the API key line only.
	ChatGPT *chatgpt.Service
}

// Sources are the sources of the cards that have a route (Register) over the services.
func (s Services) Sources() map[string]Source {
	m := map[string]Source{}
	if s.ModelManager != nil {
		m[paths.ToolModelManager] = s.comfyUI(ComfyUI.ModelManagerLines)
	}
	return m
}

func (s Services) comfyUI(lines func(ComfyUI) []molecules.StatusLine) Source {
	return func(ctx context.Context) ([]molecules.StatusLine, error) {
		b, err := s.ModelManager.Brief(ctx)
		if err != nil {
			return nil, err
		}
		return lines(ComfyUI{Supported: b.Supported, Installed: b.Installed, State: b.Process, External: b.External}), nil
	}
}

// Settings is the source of the API keys card of the settings overview (which reads it itself, with no route of its own).
// It counts the keys that are set. The OS key store can hang (a Secret Service waiting for an unlock prompt, 5 s a
// call), and this runs on after the card gave up at the deadline, so each provider and the ChatGPT read are asked for
// only while the request is still waiting: an abandoned read stops after the call it is in.
func (s Services) Settings(ctx context.Context) ([]molecules.StatusLine, error) {
	configured := 0
	var keysErr error
	for _, p := range service.CredentialProviders {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ok, err := s.Credentials.Configured(p.ID)
		if err != nil {
			keysErr = err
			break
		}
		if ok {
			configured++
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return SettingsLines(configured, len(service.CredentialProviders), keysErr, s.login()), nil
}

// login is the ChatGPT sign-in over the official, the image generation and the Codex methods, or nil when it cannot be
// read (no service, or the token store does not answer). The tokens are not read: only the facts of the sign-ins.
func (s Services) login() *Login {
	if s.ChatGPT == nil {
		return nil
	}
	ov, err := s.ChatGPT.Overview()
	if err != nil {
		return nil
	}
	l := Login{
		SignedIn:     ov.Official.SignedIn || ov.Image.SignedIn,
		NeedsRelogin: ov.Official.NeedsRelogin || ov.Image.NeedsRelogin,
	}
	if cx := s.ChatGPT.Codex(); cx != nil {
		l.SignedIn = l.SignedIn || cx.Status().Account.SignedIn
	}
	return &l
}
