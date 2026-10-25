package codex

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// LogoutResult is what a logout did.
type LogoutResult struct {
	// WasSignedIn is whether the account was signed in as far as mam knew.
	WasSignedIn bool
	// LoggedOut is whether the app-server signed out (it ended the sign-in and removed its stored credentials).
	LoggedOut bool
	// Removed is whether the engine, CODEX_HOME and the record were deleted (RemoveFiles).
	Removed bool
	// CredentialsMayRemain is true when files were removed without a sign-out because the engine was not there: a
	// credential in the OS keyring can be left, and the user should remove it by hand or sign in and out again.
	CredentialsMayRemain bool
}

// Logout signs out (account/logout), stops the app-server and, with removeFiles, deletes the downloaded engine, mam's
// CODEX_HOME and the record. A sign-out the app-server refuses is an error and nothing is deleted: the credentials
// would be left in the keyring with no way for mam to remove them. When there is no engine to ask, a removal still
// goes on and the result says the credentials may remain. A custom path is forgotten, the file is never deleted.
func (s *Service) Logout(ctx context.Context, removeFiles bool) (LogoutResult, error) {
	res := LogoutResult{WasSignedIn: s.state.get().Account.SignedIn}
	_, _, exeErr := s.exePath()
	if exeErr == nil {
		se, err := s.session(ctx)
		if err != nil {
			return res, err
		}
		var acc protocol.GetAccountResult
		if err := se.Call(ctx, protocol.MethodAccountRead, protocol.GetAccountParams{RefreshToken: false}, &acc); err == nil && acc.Account != nil {
			res.WasSignedIn = true
		}
		if err := se.Call(ctx, protocol.MethodLogout, struct{}{}, nil); err != nil {
			return res, fmt.Errorf("codex: account/logout: %w", err)
		}
		res.LoggedOut = true
	} else if !errors.Is(exeErr, ErrEngineNotInstalled) {
		return res, exeErr
	}
	s.setAccount(AccountView{SignedIn: false})
	s.forgetModels()
	if l, ok := s.CurrentLogin(); ok && l.State == LoginPending {
		s.finishLogin(l.ID, LoginCancelled, "ログアウトしたため中止しました")
	}
	s.srv.Stop()
	if !removeFiles {
		return res, nil
	}
	if err := s.removeFiles(); err != nil {
		return res, err
	}
	res.Removed = true
	res.CredentialsMayRemain = !res.LoggedOut && res.WasSignedIn
	return res, nil
}

func (s *Service) removeFiles() error {
	s.CancelInstall()
	s.WaitInstall()
	if s.installer != nil {
		if err := s.installer.Remove(); err != nil {
			return err
		}
	}
	for _, d := range []string{s.home, s.work} {
		if err := os.RemoveAll(d); err != nil {
			return fmt.Errorf("codex: remove %s: %w", d, err)
		}
	}
	s.mu.Lock()
	s.install = installState{}
	s.credsStore = ""
	s.mu.Unlock()
	return s.state.remove()
}
