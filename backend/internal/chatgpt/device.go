package chatgpt

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/oauth"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/tokenstore"
)

// StartDeviceLogin starts the device-code login: the user opens the verification URL on any device and enters the code.
// Only the Codex client has this flow; the official client answers ErrNoDeviceLogin (its server refuses the request:
// invalid_client), and a remote user signs in on the machine mam runs on. The login runs in the background.
func (a *Account) StartDeviceLogin(ctx context.Context) (Login, error) {
	p := a.profile("")
	if p.DeviceBase == "" {
		return Login{}, ErrNoDeviceLogin
	}
	dc, err := a.client.RequestDeviceCode(ctx, p)
	if errors.Is(err, oauth.ErrNoDeviceCode) {
		return Login{}, ErrNoDeviceLogin
	}
	if err != nil {
		return Login{}, err
	}
	reg, err := a.registration()
	if err != nil {
		return Login{}, err
	}
	pollCtx, cancel := context.WithDeadline(context.Background(), dc.ExpiresAt)
	id := a.start(MethodDevice, cancel)
	a.update(id, func(l *Login) {
		l.VerificationURL, l.UserCode, l.Interval, l.ExpiresAt = dc.VerificationURL, dc.UserCode, dc.Interval, dc.ExpiresAt
		l.Message = "別の端末でも構いません。表示した URL を開き、コードを入力してください。自分で始めたログインのときだけ続けてください"
	})
	go func() {
		defer cancel()
		grant, err := a.client.PollDevice(pollCtx, p, dc)
		if err == nil {
			err = a.complete(pollCtx, grant2(p, grant, reg))
		}
		a.finish(id, err)
	}()
	login, _ := a.LoginStatus(id)
	return login, nil
}

func grant2(p oauth.Profile, g oauth.DeviceGrant, reg tokenstore.Registration) grant {
	return grant{profile: p, clientID: p.ClientID, code: g.Code, verifier: g.Verifier, redirectURI: p.DeviceCallbackURI(), reg: reg}
}
