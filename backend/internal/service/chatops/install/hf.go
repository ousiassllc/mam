package install

import (
	"context"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
)

// HF is the Handler of OpInstallHFFile.
type HF struct{ deps HFDeps }

// NewHF returns the handler of OpInstallHFFile.
func NewHF(d HFDeps) *HF { return &HF{deps: d} }

const whichRepo = "どの Hugging Face のリポジトリですか？「huggingface.co/owner/name の safetensors を入れて」のように、リポジトリ（owner/name か URL）を添えて送ってください。"

// Question reads the repository as get_hf_model_files does (the commit is the one the Hub gives now, never one the model wrote), picks the file, and shows its size, the
// folder it goes to, the license the model card declares and the terms before anything starts. Several installable files are a card of buttons.
func (h *HF) Question(ctx context.Context, _ domain.Project, op commandinterpreter.Operation) (chatops.Card, error) {
	var a HFArgs
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Card{}, err
	}
	if strings.TrimSpace(a.Repo) == "" {
		return chatops.Card{}, chatops.Refuse("%s", whichRepo)
	}
	env, err := h.deps.Environment(ctx)
	if err != nil {
		return chatops.Card{}, err
	}
	if err := environment(env); err != nil {
		return chatops.Card{}, err
	}
	repo, err := h.deps.Repo(ctx, a.Repo)
	if err != nil {
		return chatops.Card{}, applyRefusal(err)
	}
	file, candidates, err := pickFile(repo, a.Path)
	switch {
	case err != nil:
		return chatops.Card{}, err
	case len(candidates) > 0:
		ops := make([]commandinterpreter.Operation, len(candidates))
		for i, c := range candidates {
			if ops[i], err = commandinterpreter.NewOperation(OpInstallHFFile, HFArgs{Repo: repo.ID, Path: c.ID, Pick: true}); err != nil {
				return chatops.Card{}, err
			}
		}
		card := choiceCard("ファイル", names(candidates), ops)
		if repo.Truncated {
			card.Text += "\n" + truncatedNote(repo)
		}
		return card, nil
	}
	return h.confirm(repo, file, env)
}

// pickFile is the file the director named, or the only one that can be installed, or the files to choose from (the installable ones, or the ones that share the name given).
func pickFile(repo HFRepo, path string) (HFFile, []named, error) {
	if path = strings.TrimSpace(path); path != "" {
		for _, f := range repo.Files { // the exact path first: "model.safetensors" is also the Name of "a/model.safetensors"
			if strings.EqualFold(f.Path, path) {
				return f, nil, nil
			}
		}
		var same, ok []HFFile // the files of that name in other folders: which one is asked, not taken as the first; only the installable ones are offered
		for _, f := range repo.Files {
			if strings.EqualFold(f.Name, path) {
				same = append(same, f)
				if f.Installable {
					ok = append(ok, f)
				}
			}
		}
		switch {
		case len(same) == 1:
			return same[0], nil, nil
		case len(ok) == 1:
			return ok[0], nil, nil // the others cannot be installed
		case len(same) > 1 && len(ok) == 0:
			return same[0], nil, nil // none can be installed: the card of the first says why
		case len(ok) > chatops.MaxChoices:
			return HFFile{}, nil, tooManySameName(path, ok)
		case len(ok) > 1:
			return HFFile{}, pathChoices(ok), nil
		}
		return HFFile{}, nil, chatops.Refuse("%s に「%s」というファイルはありません%s", repo.ID, path, listNote(repo))
	}
	if repo.Selected != "" {
		for _, f := range repo.Files {
			if f.Path == repo.Selected {
				return f, nil, nil
			}
		}
	}
	var ok []HFFile
	for _, f := range repo.Files {
		if f.Installable && !f.Installed {
			ok = append(ok, f)
		}
	}
	switch {
	case len(ok) == 1:
		return ok[0], nil, nil
	case len(ok) == 0 && len(repo.Files) == 0:
		return HFFile{}, nil, chatops.Refuse("%s に導入できるファイル（.safetensors）がありません%s", repo.ID, listNote(repo))
	case len(ok) == 0:
		return repo.Files[0], nil, nil // each is installed already or cannot be: the card of the first says which
	}
	if len(ok) > chatops.MaxChoices {
		return HFFile{}, nil, tooMany(repo.ID, names(pathChoices(ok)))
	}
	return HFFile{}, pathChoices(ok), nil
}

// pathChoices lists the files as choices named by their path (the path with its folders is what tells same-named files apart).
func pathChoices(files []HFFile) []named {
	found := make([]named, len(files))
	for i, f := range files {
		found[i] = named{ID: f.Path, Name: f.Path}
	}
	return found
}

// truncatedNote tells that the list of files was cut: what the card offers may not be everything the repository has.
func truncatedNote(repo HFRepo) string {
	return "・このリポジトリはファイルが多く、一覧は途中までです。ほかに導入できるファイルがあるかもしれません。リポジトリのページ（" + repo.Page + "）で確かめてください。"
}

// listNote ends a refusal that says a file is not there: when the list was cut, the file may be beyond it ("" otherwise).
func listNote(repo HFRepo) string {
	if repo.Truncated {
		return "（このリポジトリはファイルが多く、一覧は途中までしか読めていません。リポジトリのページ " + repo.Page + " で確かめてください）"
	}
	return ""
}

// confirm is the card of one file. The yes carries the commit the card shows; when the terms are not confirmed for this commit the button is "同意して導入する", which also
// carries the version of the terms shown, and it is the confirmation the install records.
func (h *HF) confirm(repo HFRepo, f HFFile, env Environment) (chatops.Card, error) {
	switch {
	case f.Installed:
		return chatops.Card{}, chatops.ErrNoChange
	case f.InstallJob != "":
		return chatops.Card{}, installing(f.Name)
	case !f.Installable:
		return chatops.Card{}, chatops.Refuse("%s の %s は導入できません: %s", repo.ID, f.Path, f.Reason)
	case f.Folder == "":
		return chatops.Card{}, chatops.Refuse("%s の導入先のフォルダを決められませんでした。モデルマネージャーの画面（Hugging Face から追加）で、フォルダを選んで導入してください", f.Path)
	}
	if repo.Gated && !repo.TokenConfigured {
		return chatops.Card{}, chatops.Refuse("%s は gated（アクセスの承認が要る）リポジトリです。Hugging Face のトークンを設定し、%s でアクセスを申請してください", repo.ID, repo.Page)
	}
	if err := room(f.Size, env.FreeBytes); err != nil {
		return chatops.Card{}, err
	}
	text := "Hugging Face の「" + repo.ID + "」から " + f.Path + " を導入しますね？\n" +
		"・サイズ: " + bytesText(f.Size) + "。導入先: models/" + f.Folder + "/" + f.Name + "\n" +
		"・コミット: " + shortCommit(repo.Commit) + "（この版に固定して取得し、SHA-256 とサイズで検証します）\n"
	if env.FreeBytes > 0 {
		text += "・導入先の空き容量: " + bytesText(signed(env.FreeBytes)) + "\n"
	}
	text += strings.Replace(licenseLines(repo.License), "ライセンス:", "ライセンス（モデルカードの自己申告）:", 1)
	if repo.License.URL != "" {
		text += "・ライセンスの場所: " + repo.License.URL + "\n"
	}
	if repo.LicenseWarning != "" {
		text += "・" + repo.LicenseWarning + "\n"
	}
	if f.Security != "" {
		text += "・Hugging Face のセキュリティスキャン: " + f.Security + "\n"
	}
	if repo.NFAA {
		text += "・成人向け（NFAA）のリポジトリです。\n"
	}
	if repo.Gated {
		text += "・アクセス承認が必要なリポジトリです（設定した Hugging Face のトークンで取得します）。\n"
	}
	if repo.Truncated {
		text += truncatedNote(repo) + "\n"
	}
	args := HFArgs{Repo: repo.ID, Path: f.Path, Commit: repo.Commit}
	if repo.Consented {
		return yes(OpInstallHFFile, args, text+globalNote)
	}
	args.TermsVersion = repo.TermsVersion
	op, err := commandinterpreter.NewOperation(OpInstallHFFile, args)
	if err != nil {
		return chatops.Card{}, err
	}
	text += "・" + repo.Terms + "\n・下の「" + consentLabel + "」を押すと、この注意書き（版 " + repo.TermsVersion + "）に同意したことになります。同意はこのリポジトリのこのコミットごとに一度です。\n"
	return chatops.Card{Text: text + globalNote, Choices: []chatops.Choice{{Label: consentLabel, Op: op}}}, nil
}

// yes is a card whose yes button carries args.
func yes(t commandinterpreter.OpType, args any, text string) (chatops.Card, error) {
	op, err := commandinterpreter.NewOperation(t, args)
	return chatops.Card{Text: text, Op: &op}, err
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// Apply queues the install as install_hf_file does, with the commit and the terms version the card showed. A button that offered several files only asks the confirmation
// of the one picked.
func (h *HF) Apply(ctx context.Context, project domain.Project, op commandinterpreter.Operation) (chatops.Result, error) {
	var a HFArgs
	if err := op.DecodeArgs(&a); err != nil {
		return chatops.Result{}, err
	}
	if a.Pick {
		pick, err := commandinterpreter.NewOperation(OpInstallHFFile, HFArgs{Repo: a.Repo, Path: a.Path})
		if err != nil {
			return chatops.Result{}, err
		}
		card, err := h.Question(ctx, project, pick)
		if errors.Is(err, chatops.ErrNoChange) {
			return chatops.Result{Text: "「" + a.Path + "」はすでに導入済みです。"}, nil
		} else if err != nil {
			return chatops.Result{}, err
		}
		msg, err := chatops.Message(pick, card)
		return chatops.Result{Text: "「" + a.Path + "」ですね。導入の前に確かめてください。", Messages: []domain.ChatMessage{msg}}, err
	}
	if a.Commit == "" {
		return chatops.Result{}, chatops.Refuse("取得するコミットが決まっていません。もう一度「%s の safetensors を入れて」と送って、カードを確かめてください", a.Repo)
	}
	job, err := h.deps.Install(ctx, a.Repo, a.Commit, a.Path, a.TermsVersion)
	if err != nil {
		return chatops.Result{}, applyRefusal(err)
	}
	return started(job, a.Path), nil
}
