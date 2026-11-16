package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/mam/backend/internal/agent/gentestchat"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/audiolength"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/secretguard"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// The kinds of file a conversation takes (Issue #1153): ONE sound and ONE picture. A speech-to-video model cannot run without both, and the chat is the only place the
// LLM has no way to hand them over, so the user attaches them and the LLM only says "use what is attached" (Batch.Audio, Batch.Image).
const (
	AttachAudio = gentestchat.AttachAudio
	AttachImage = gentestchat.AttachImage
)

// maxNameRunes bounds the file name that is shown to the LLM and in the thread.
const maxNameRunes = 80

// attachment is a file of the conversation. id is the staged id of the master copy: it is never given to a batch (every estimate stages a copy of it, so that the batch that
// runs consumes the copy and the file stays for the next estimate); it lives in the staging folder, which is swept like every staged upload, and in memory only here.
type attachment struct {
	id      string
	name    string
	seconds float64 // a sound's length
}

// Attach takes a file the user attached to the conversation (kind AttachAudio or AttachImage; one of each kind: a new file replaces the old one) and puts it in staging
// like the file inputs of the form do (gentest.Service.StageAudio / StageReference), so that its kind, size and (a sound) length are refused with the form's words. The
// reply is the line the thread shows. The LLM learns of the file with its next message: it is told the name (data, cleaned of control characters and keys) and the length,
// never the contents or the id.
func (s *Service) Attach(_ context.Context, sessionID, kind, name string, data []byte) (Reply, error) {
	se, err := s.open(sessionID, true)
	if err != nil {
		return Reply{}, err
	}
	var (
		id      string
		seconds float64
		label   string
	)
	switch kind {
	case AttachAudio:
		label = "音声ファイル"
		if id, _, err = s.d.Gentest.StageAudio(data); err != nil {
			return Reply{}, err
		}
		if seconds, err = audiolength.Of(data, s.d.Gentest.Adapter.Config().Gentest.AudioSeconds()); err != nil {
			s.discardFile(id)
			return Reply{}, err
		}
	case AttachImage:
		label = "参照画像"
		if id, _, err = s.d.Gentest.StageReference(data); err != nil {
			return Reply{}, err
		}
	default:
		return Reply{}, gterr.Invalid("添付できるのは音声ファイルか参照画像です")
	}
	a := &attachment{id: id, name: cleanName(name), seconds: seconds}
	se.mu.Lock()
	old := se.files[kind]
	if se.files == nil {
		se.files = map[string]*attachment{}
	}
	se.files[kind] = a
	se.mu.Unlock()
	if old != nil {
		s.discardFile(old.id)
	}
	text := fmt.Sprintf("%sを添付しました: %s", label, a.name)
	if kind == AttachAudio {
		text += fmt.Sprintf("（%.1f 秒）", seconds)
	}
	return Reply{Messages: []Message{{ID: newID(), Kind: KindStatus, Text: text + "。これを使う見積りを、チャットで頼めます"}}}, nil
}

// cleanName is the name of an attached file as the LLM and the thread are shown it: no folder, no control character, at most maxNameRunes, no API key.
func cleanName(name string) string {
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name, _ = secretguard.Redact(strings.TrimSpace(name))
	if utf8.RuneCountInString(name) > maxNameRunes {
		name = string([]rune(name)[:maxNameRunes])
	}
	if name == "" {
		name = "(名前なし)"
	}
	return name
}

// attachments are the files the LLM is told of, the sound first.
func (se *session) attachments() []gentestchat.Attachment {
	se.mu.Lock()
	defer se.mu.Unlock()
	var out []gentestchat.Attachment
	for _, kind := range []string{AttachAudio, AttachImage} {
		if a := se.files[kind]; a != nil {
			out = append(out, gentestchat.Attachment{Kind: kind, Name: a.name, Seconds: a.seconds})
		}
	}
	return out
}

// used names the attached files an estimate uses, as the card shows them.
type used struct{ audio, image string }

// inputs puts a copy of the attached files the batch asks for (Batch.Audio, Batch.Image) in the request as staged inputs, a sound with the role audio and a picture
// with the role reference. When it cannot (nothing is attached, or the staged file is gone) it says why twice: ui for the thread (Japanese), llm for the LLM.
func (s *Service) inputs(se *session, b gentestchat.Batch, r *gentest.Request) (u used, ui, llm string) {
	for _, w := range []struct {
		want       bool
		kind, role string
		label, ja  string
	}{{b.Audio, AttachAudio, gentest.RoleAudio, "audio file", "音声ファイル"}, {b.Image, AttachImage, gentest.RoleReference, "reference picture", "参照画像"}} {
		if !w.want {
			continue
		}
		se.mu.Lock()
		a := se.files[w.kind]
		se.mu.Unlock()
		if a == nil {
			return u, w.ja + "が添付されていません", "the user has attached no " + w.label + " to this conversation. Ask the user in Japanese to attach one with the button next to the message box, and estimate again after the user says it is attached."
		}
		id, err := s.copyOf(a, w.kind)
		if err != nil {
			s.forget(se, w.kind, a)
			return u, "添付した" + w.ja + "が使えなくなりました（24 時間を過ぎたか、取り除かれました）。もう一度添付してください", "the " + w.label + " the user attached is not available any more. Ask the user in Japanese to attach it again."
		}
		r.References = append(r.References, gentest.RefInput{Source: gentest.FromStaged, ID: id, Role: w.role})
		if w.kind == AttachAudio {
			u.audio = fmt.Sprintf("%s（%.1f 秒）", a.name, a.seconds)
		} else {
			u.image = a.name
		}
	}
	return u, "", ""
}

// copyOf stages a new copy of an attached file, which one batch consumes.
func (s *Service) copyOf(a *attachment, kind string) (string, error) {
	path, _, err := s.d.Gentest.StagedFile(a.id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path of a staged file the service named: its id has no separator (stagedPattern)
	if err != nil {
		return "", err
	}
	if kind == AttachAudio {
		id, _, err := s.d.Gentest.StageAudio(data)
		return id, err
	}
	id, _, err := s.d.Gentest.StageReference(data)
	return id, err
}

// forget drops the attachment of a kind when it is still a.
func (s *Service) forget(se *session, kind string, a *attachment) {
	se.mu.Lock()
	defer se.mu.Unlock()
	if se.files[kind] == a {
		delete(se.files, kind)
	}
}

// discard removes the staged copies a request holds, for a request that will not run (a refused estimate, a card that was closed): a request that runs consumes them.
func (s *Service) discard(refs []gentest.RefInput) {
	for _, r := range refs {
		if r.Source == gentest.FromStaged {
			s.discardFile(r.ID)
		}
	}
}

func (s *Service) discardFile(id string) {
	if path, _, err := s.d.Gentest.StagedFile(id); err == nil {
		if err := os.Remove(filepath.Clean(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.d.Gentest.Logger.Warn("gentest chat: a staged file could not be removed", "err", err)
		}
	}
}
