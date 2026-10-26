package codextest

import (
	"encoding/json"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

func (f *Fake) answer(method string, params json.RawMessage) Reply {
	f.mu.Lock()
	override := f.Override[method]
	f.mu.Unlock()
	if override != nil {
		return override(params)
	}
	switch method {
	case protocol.MethodInitialize:
		return Reply{Result: protocol.InitializeResult{CodexHome: f.CodexHome}}
	case protocol.MethodAccountRead:
		return Reply{Result: protocol.GetAccountResult{Account: f.Account, RequiresOpenAIAuth: true}}
	case protocol.MethodRateLimitsRead:
		return Reply{Result: f.RateLimits}
	case protocol.MethodModelList:
		return Reply{Result: protocol.ModelListResult{Data: f.Models}}
	case protocol.MethodLoginStart:
		var lp protocol.LoginParams
		_ = json.Unmarshal(params, &lp)
		if f.Login != nil {
			return f.Login(lp.Type)
		}
		if lp.Type == protocol.LoginDeviceCode {
			return Reply{Result: protocol.LoginResult{Type: lp.Type, LoginID: "login-1", UserCode: "ABCD-1234", VerificationURL: "https://auth.example.invalid/codex/device"}}
		}
		return Reply{Result: protocol.LoginResult{Type: lp.Type, LoginID: "login-1", AuthURL: "https://auth.example.invalid/authorize?state=x"}}
	case protocol.MethodLoginCancel:
		return Reply{Result: map[string]string{"status": "canceled"}}
	case protocol.MethodLogout:
		f.mu.Lock()
		f.Account = nil
		f.mu.Unlock()
		go f.Notify(protocol.NotifAccountUpdated, map[string]any{"authMode": nil, "planType": nil})
		return Reply{Result: struct{}{}}
	case protocol.MethodThreadStart:
		var tp protocol.ThreadStartParams
		_ = json.Unmarshal(params, &tp)
		id := f.newID("thread")
		f.mu.Lock()
		f.threads = append(f.threads, ThreadStart{ThreadID: id, Params: tp})
		f.mu.Unlock()
		res := protocol.ThreadStartResult{}
		res.Thread.ID = id
		return Reply{Result: res}
	case protocol.MethodTurnStart:
		return Reply{Result: protocol.TurnStartResult{Turn: protocol.TurnRef{ID: f.newID("turn"), Status: "inProgress"}}}
	case protocol.MethodThreadUnsubscribe, protocol.MethodTurnInterrupt:
		return Reply{Result: struct{}{}}
	}
	return Reply{Err: &RPCError{Code: -32601, Message: "method not found: " + method}}
}
