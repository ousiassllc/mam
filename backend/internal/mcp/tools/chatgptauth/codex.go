package chatgptauth

import (
	"context"
	"errors"
	"time"

	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex"
	"github.com/ousiassllc/mam/backend/internal/chatgpt/codex/engine"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// KindCodexInstall is the job kind of the download of the Codex app-server.
const KindCodexInstall = "chatgpt_codex_install"

// registerCodex adds the tools of the Codex login (text generation through the Codex app-server: any plan, workspace
// accounts included) and the Probe of its engine install.
func registerCodex(r *mcpkit.Registrar, h handlers) {
	r.Jobs.RegisterKind(KindCodexInstall, h.probeInstall, mcpkit.InMemory())
	mcpkit.Write(r, "install_chatgpt_codex_engine",
		"Codex 経由のログインに使う Codex app-server（OpenAI 公式。Apache-2.0）を、公式の GitHub リリースから codex ディレクトリ（Windows 版は mam.exe と同じ場所、macOS 版はデータディレクトリの中）にダウンロードする（約 75〜90 MB。SHA-256 とサイズを確かめる。Windows 版と macOS 版（Apple Silicon）のみ）。job_id を返すので get_job で進捗を見る。中断しても続きから再開する / Download the pinned Codex app-server (Windows and macOS Apple Silicon only) and poll it with get_job.",
		h.installEngine, mcpkit.OpenWorld())
	mcpkit.Write(r, "start_chatgpt_codex_login",
		"Codex 経由で ChatGPT にログインする（テキスト生成のみ。Business / Enterprise / Edu のワークスペースのアカウントも使える。LLM の割り当てのプロバイダー chatgpt-codex）。method=browser は mam が動いている PC の既定ブラウザを開き、device はユーザーコードと URL を返す（ワークスペースでデバイスコードの許可が必要）。ローカル・オープンソースの利用のみで、商用・ホスティングでは使えない。完了は get_chatgpt_login_status / Start the Codex login (text generation only; workspace accounts work). Local, open-source use only.",
		h.startCodexLogin, mcpkit.OpenWorld())
}

// CodexAccount is the Codex login and its engine. It holds no token.
type CodexAccount struct {
	SignedIn      bool        `json:"signed_in"`
	Email         string      `json:"email,omitempty"`
	Plan          string      `json:"plan,omitempty" jsonschema:"ChatGPT のプラン（business / enterprise / edu / plus など。Codex が伝えるとき）"`
	Engine        string      `json:"engine" jsonschema:"エンジンの状態: not_installed / installing / ready / running / failed"`
	EngineVersion string      `json:"engine_version,omitempty"`
	EngineSource  string      `json:"engine_source,omitempty" jsonschema:"download（公式のリリースから取得）または custom（指定したパスの codex）"`
	EngineError   string      `json:"engine_error,omitempty"`
	CanDownload   bool        `json:"can_download" jsonschema:"このアプリでエンジンをダウンロードできる（Windows 版と macOS 版（Apple Silicon）のみ）"`
	RateLimits    []CodexRate `json:"rate_limits,omitempty" jsonschema:"Codex の利用上限（最後に読んだ値）"`
}

// CodexRate is one usage window of the Codex usage limit.
type CodexRate struct {
	Name        string `json:"name"`
	UsedPercent int    `json:"used_percent"`
	ResetsAt    string `json:"resets_at,omitempty" jsonschema:"リセット時刻（RFC 3339）"`
}

func codexAccount(s *codex.Service) CodexAccount {
	st := s.Status()
	a := CodexAccount{
		SignedIn: st.Account.SignedIn, Email: st.Account.Email, Plan: st.Account.PlanType, Engine: string(st.Engine),
		EngineVersion: st.Version, EngineSource: string(st.Source), EngineError: st.EngineError, CanDownload: st.Supported,
	}
	for _, w := range st.RateLimits {
		r := CodexRate{Name: w.Name, UsedPercent: w.UsedPercent}
		if !w.ResetsAt.IsZero() {
			r.ResetsAt = w.ResetsAt.UTC().Format(time.RFC3339)
		}
		a.RateLimits = append(a.RateLimits, r)
	}
	return a
}

// CodexInstallOutput is the install job.
type CodexInstallOutput struct {
	mcpkit.JobRef
	Existing bool `json:"existing,omitempty" jsonschema:"ダウンロードがすでに動いていたので、それを追う / one was already running"`
}

func (h handlers) installEngine(ctx context.Context, _ Input) (CodexInstallOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return CodexInstallOutput{}, err
	}
	var started bool
	ref, err := h.r.Jobs.Run(ctx, KindCodexInstall, "codex-engine", func(context.Context) (string, error) {
		var err error
		started, err = svc.Codex().StartInstall()
		return "", mapInstallError(err)
	})
	return CodexInstallOutput{JobRef: ref, Existing: err == nil && !started}, err
}

// mapInstallError turns the refusals of an install into the conflicts the tools report.
func mapInstallError(err error) error {
	var noSpace *fetch.NoSpaceError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, codex.ErrUnsupported):
		return &service.ConflictError{Message: "この環境ではダウンロードできません（Windows 版と macOS 版（Apple Silicon）のみ）。自分で入れた codex のパスを設定画面で指定してください"}
	case errors.Is(err, engine.ErrNotWritable):
		return &service.ConflictError{Message: "Codex の保存先（Windows 版は mam の実行ファイルと同じ場所、macOS 版はデータディレクトリの codex ディレクトリ）に書き込めません"}
	case errors.As(err, &noSpace):
		return &service.ConflictError{Message: "ディスクの空きが足りません"}
	}
	return err
}

// probeInstall reads the progress of the install from the Codex service.
func (h handlers) probeInstall(context.Context, mcpkit.JobInfo) (mcpkit.Progress, error) {
	svc, err := h.svc()
	if err != nil {
		return mcpkit.Progress{}, err
	}
	st := svc.Codex().InstallStatus()
	p := mcpkit.Progress{Message: st.Message, Done: st.Percent, Total: 100}
	switch st.State {
	case codex.InstallSucceeded:
		p.State = mcpkit.JobSucceeded
		p.Result = map[string]any{"engine": "ready"}
	case codex.InstallFailed:
		p.State, p.Error = mcpkit.JobFailed, st.Error
	case codex.InstallRunning:
		p.State = mcpkit.JobRunning
	default:
		p.State = mcpkit.JobCancelled
	}
	return p, nil
}

func (h handlers) startCodexLogin(ctx context.Context, in CodexLoginInput) (LoginOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return LoginOutput{}, err
	}
	method := codex.MethodBrowser
	switch in.Method {
	case "", "browser":
	case "device":
		method = codex.MethodDevice
	default:
		return LoginOutput{}, &service.ValidationError{Field: "method", Message: "browser か device を指定してください"}
	}
	l, err := svc.Codex().StartLogin(ctx, method)
	var ls *codex.LoginStartError
	switch {
	case errors.Is(err, codex.ErrEngineNotInstalled):
		return LoginOutput{}, &service.ConflictError{Message: "Codex のエンジンがありません。install_chatgpt_codex_engine でダウンロードするか、設定画面で自分で入れた codex のパスを指定してください"}
	case errors.Is(err, codex.ErrEngineInstalling):
		return LoginOutput{}, &service.ConflictError{Message: "Codex のエンジンを導入中です。終わってからもう一度試してください"}
	case errors.As(err, &ls):
		return LoginOutput{}, &service.ConflictError{Message: ls.Message}
	case err != nil:
		return LoginOutput{}, err
	}
	out := codexLoginOutput(l)
	out.Note = "ブラウザは mam が動いている PC で開きます。デバイスコードは利用者が自分で始めたときだけ入力してもらいます。ワークスペースのアカウントは、管理者が Codex の利用（Codex Local、デバイスコードのログイン）を許可している必要があります。ログイン後は LLM の割り当てでプロバイダーを chatgpt-codex にすると使えます（自動では切り替わりません）。"
	return out, nil
}

// CodexLoginInput is the method of a Codex login.
type CodexLoginInput struct {
	Method string `json:"method,omitempty" jsonschema:"browser（既定。mam が動いている PC のブラウザ）または device（デバイスコード）"`
}

func codexLoginOutput(l codex.Login) LoginOutput {
	out := LoginOutput{
		LoginID: l.ID, Account: "codex", Method: string(l.Method), State: string(l.State), Message: l.Message,
		AuthURL: l.AuthURL, BrowserOpened: l.BrowserOpened, VerificationURL: l.VerificationURL, UserCode: l.UserCode,
		IntervalSeconds: 2,
	}
	if !l.ExpiresAt.IsZero() {
		out.ExpiresAt = l.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return out
}

// logoutCodex signs out of Codex; removeFiles also deletes the downloaded engine and mam's own CODEX_HOME.
func (h handlers) logoutCodex(ctx context.Context, removeFiles bool) (LogoutOutput, error) {
	svc, err := h.svc()
	if err != nil {
		return LogoutOutput{}, err
	}
	res, err := svc.Codex().Logout(ctx, removeFiles)
	if err != nil {
		return LogoutOutput{}, err
	}
	out := LogoutOutput{Account: "codex", WasSignedIn: res.WasSignedIn, RevokeConfirmed: res.LoggedOut}
	switch {
	case res.CredentialsMayRemain:
		out.Message = "ファイルを削除しました。ただしエンジンが無かったため、OS の資格情報ストアに Codex の資格情報が残っている可能性があります。Windows の「資格情報マネージャー」で Codex を探して削除するよう利用者に伝えてください"
	case res.Removed:
		out.Message = "ログアウトし、取得した Codex 本体と mam 専用の CODEX_HOME を削除しました"
	case res.LoggedOut:
		out.Message = "ログアウトしました。Codex が保存した資格情報を消しました"
	default:
		out.Message = "ログインしていませんでした"
	}
	return out, nil
}
