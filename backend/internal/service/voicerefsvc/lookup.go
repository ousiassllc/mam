package voicerefsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// Why a speaker has no reference voice (errors.Is): the local voice provider tells the user which one it is.
var (
	// ErrNoCharacter: no Character of the Project has the speaker's name.
	ErrNoCharacter = errors.New("voicerefsvc: no character has that name")
	// ErrNoReference: the Character exists but has no reference voice.
	ErrNoReference = errors.New("voicerefsvc: the character has no reference voice")
	// ErrAmbiguous: several Characters of that name have different reference voices; the name does not say which.
	ErrAmbiguous = errors.New("voicerefsvc: several characters of that name have different reference voices")
	// ErrFileMissing: the reference voice is set but its file is gone (deleted from the assets directory).
	ErrFileMissing = errors.New("voicerefsvc: the reference voice file is missing")
)

// Voice is the reference voice of the Character a speaker stands for.
type Voice struct {
	Character domain.WorldCharacter
	Reference voiceref.Reference
	// Path is the absolute path of the recording.
	Path string
}

// ForSpeaker finds the reference voice of the speaker of a line of the script: the Character of the Project
// (of its Branch, "" for the trunk) whose name is the speaker's, ignoring the letter case and the spaces around
// it, as the Reference Pack finds the face of a Character by its name. It is what a local voice provider uses so
// that the same Character has the same voice in every line.
//
// Several Characters with that name are fine when one recording is the voice of all of them (the Episode and its
// copies in a Branch share the file) or when only one of them has a reference voice; Characters with different
// recordings are ErrAmbiguous. The Project (or Branch) that does not exist is ErrNotFound. ErrNoCharacter,
// ErrNoReference and ErrFileMissing say why there is no voice. The narration is the Character named
// voiceref.NarrationName: see ForNarration.
func (s *Service) ForSpeaker(ctx context.Context, projectID, branchID, speaker string) (Voice, error) {
	w, err := s.world.World(ctx, projectID, branchID)
	if err != nil {
		return Voice{}, notFound(err)
	}
	return s.resolve(w.Characters, speaker)
}

// ForNarration is ForSpeaker for the narrator.
func (s *Service) ForNarration(ctx context.Context, projectID, branchID string) (Voice, error) {
	return s.ForSpeaker(ctx, projectID, branchID, voiceref.NarrationName)
}

func (s *Service) resolve(characters []domain.WorldCharacter, speaker string) (Voice, error) {
	speaker = strings.TrimSpace(speaker)
	var named, withVoice []domain.WorldCharacter
	for _, c := range characters {
		if speaker == "" || !strings.EqualFold(speaker, strings.TrimSpace(c.Name)) {
			continue
		}
		named = append(named, c)
		if c.VoiceReference != nil {
			withVoice = append(withVoice, c)
		}
	}
	if len(named) == 0 {
		return Voice{}, fmt.Errorf("%w: %q", ErrNoCharacter, speaker)
	}
	if len(withVoice) == 0 {
		return Voice{}, fmt.Errorf("%w: %q", ErrNoReference, speaker)
	}
	for _, c := range withVoice[1:] {
		if c.VoiceReference.FilePath != withVoice[0].VoiceReference.FilePath {
			return Voice{}, fmt.Errorf("%w: %q", ErrAmbiguous, speaker)
		}
	}
	v := Voice{Character: withVoice[0], Reference: *withVoice[0].VoiceReference}
	if v.Path = s.path(v.Reference.FilePath); v.Path != "" {
		if _, err := os.Stat(v.Path); err == nil {
			return v, nil
		}
	}
	return Voice{}, fmt.Errorf("%w: %q", ErrFileMissing, speaker)
}
