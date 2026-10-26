package appserver_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/appserver"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/codextest"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/protocol"
)

// stderrT lets the fake run in a child process, where there is no *testing.T.
type stderrT struct{}

func (stderrT) Helper()                   {}
func (stderrT) Errorf(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
func (stderrT) Error(a ...any)            { fmt.Fprintln(os.Stderr, a...) }

// TestMain turns the test binary into a fake app-server when MAM_FAKE_CODEX is set: Launch starts os.Args[0] again with the
// arguments and environment of the real one, and the fake tells the test what it got through account/read.
func TestMain(m *testing.M) {
	if os.Getenv("MAM_FAKE_CODEX") != "" {
		f := codextest.New(stderrT{}, os.Getenv("CODEX_HOME"))
		f.Account = &protocol.Account{
			Type: protocol.AccountTypeChatGPT, Email: strings.Join(os.Args[1:], " "),
			PlanType: "token=" + os.Getenv("CODEX_ACCESS_TOKEN") + ",key=" + os.Getenv("OPENAI_API_KEY") + ",cwd=" + mustGetwd(),
		}
		f.Serve(os.Stdin, os.Stdout)
		return
	}
	os.Exit(m.Run())
}

func mustGetwd() string { d, _ := os.Getwd(); return d }

// TestLaunchStartsARealProcessWithMamsHomeArgumentsAndNoSecrets runs the whole path: the real child process, its pipes,
// the handshake with the CODEX_HOME check, and what the process receives.
func TestLaunchStartsARealProcessWithMamsHomeArgumentsAndNoSecrets(t *testing.T) {
	t.Setenv("MAM_FAKE_CODEX", "1")
	t.Setenv("CODEX_ACCESS_TOKEN", "fake-access-token-from-the-users-environment")
	t.Setenv("OPENAI_API_KEY", "fake-api-key-from-the-users-environment")
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "users-own-codex"))
	root := t.TempDir()
	codexHome, work := filepath.Join(root, "codex-home"), filepath.Join(root, "work")
	for _, d := range []string{codexHome, work} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	l := appserver.Launch{Exe: os.Args[0], CodexHome: codexHome, WorkDir: work, CredentialsStore: protocol.CredentialsKeyring}
	srv := appserver.New(appserver.Options{CodexHome: codexHome, Start: l.Start})
	t.Cleanup(srv.Close)

	se, err := srv.Ensure(context.Background())
	if err != nil {
		t.Fatal(err) // the fake reports the CODEX_HOME it got: this fails if mam passed the user's
	}
	var res protocol.GetAccountResult
	if err := se.Call(context.Background(), protocol.MethodAccountRead, protocol.GetAccountParams{}, &res); err != nil {
		t.Fatal(err)
	}
	args, plan := res.Account.Email, res.Account.PlanType
	for _, want := range []string{
		"--listen stdio://", `-c cli_auth_credentials_store="keyring"`, `-c web_search="disabled"`, "-c features.apps=false",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("arguments %q lack %q", args, want)
		}
	}
	if !strings.Contains(args, "app-server --listen") {
		t.Errorf("a binary that is not the dedicated app-server is run as `<exe> app-server`: %q", args)
	}
	if strings.Contains(plan, "fake-access-token") || strings.Contains(plan, "fake-api-key") {
		t.Errorf("the user's token or key reached the app-server: %s", plan)
	}
	if !strings.Contains(plan, "cwd="+work) && !strings.Contains(plan, filepath.Base(work)) {
		t.Errorf("the process must run in the empty working directory: %s", plan)
	}
	srv.Stop()
}

func TestTheDedicatedAppServerTakesNoSubcommand(t *testing.T) {
	dir := t.TempDir()
	for exe, subcommand := range map[string]bool{
		filepath.Join(dir, "engine", "codex-app-server-x86_64-pc-windows-msvc.exe"): false,
		filepath.Join(dir, "codex-app-server"):                                      false,
		filepath.Join(dir, "codex"):                                                 true,
		filepath.Join(dir, "codex.exe"):                                             true,
	} {
		args := appserver.Launch{Exe: exe, CredentialsStore: "auto"}.Args()
		has := false
		for _, a := range args {
			has = has || a == "app-server"
		}
		if has != subcommand {
			t.Errorf("%s: args = %v", exe, args)
		}
		if args[len(args)-2] != "--listen" || args[len(args)-1] != "stdio://" {
			t.Errorf("%s: the transport is stdio: %v", exe, args)
		}
		if args[0] != "-c" {
			t.Errorf("%s: the -c settings come first: %v", exe, args)
		}
	}
}
