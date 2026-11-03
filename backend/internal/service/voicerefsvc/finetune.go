package voicerefsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain/voiceref"
)

// The trained voice of a Character (Issue #936): GPT-SoVITS weights next to the reference voice, in the same JSON of the character row
// (voiceref.Reference.Finetune). The training service (service/voicetrain) writes the weights into "voicetrain/<id>/" below the assets
// directory and then binds them here; the copies of the row (the Episodes of a canon, the Branches) carry the same JSON, so they
// share the weights and inherit the voice like they do the recording.

// ErrNoWeights is the cause when the trained voice is set but a weight file is gone or is not the size it was written with (deleted or damaged).
var ErrNoWeights = errors.New("voicerefsvc: the trained voice files are missing or damaged")

// Owner is whose Character it is: a Series (its canon) or a Project. Make one with ForSeries or ForProject.
type Owner = owner

// ID is the id of the Series or the Project, and IsSeries says which it is.
func (o owner) ID() string { return o.id }

// IsSeries reports whether the owner is a Series (the canon), not a Project.
func (o owner) IsSeries() bool { return o.series }

// ForSeries is the owner of the canonical Characters of the Series.
func ForSeries(id string) Owner { return owner{id: id, series: true} }

// ForProject is the owner of the Characters of the Project (and its Branches).
func ForProject(id string) Owner { return owner{id: id} }

// CharacterName is the name of the Character; an unknown Project, Series or Character is ErrNotFound.
func (s *Service) CharacterName(ctx context.Context, o Owner, characterID string) (string, error) {
	if err := s.exists(ctx, o); err != nil {
		return "", err
	}
	col := "project_id"
	if o.series {
		col = "series_id"
	}
	name, err := s.repo.Name(ctx, col, o.id, characterID)
	return name, notFound(err)
}

// WeightsRoot is the absolute directory that holds the directories of the trained voices ("" without an assets directory). The synthesis
// server is told this one directory and resolves a trained voice by its id below it.
func (s *Service) WeightsRoot() string { return s.path(voiceref.FinetuneDir) }

// Reference is the reference voice of the Character, nil when it has none; an unknown Project, Series or Character is ErrNotFound.
func (s *Service) Reference(ctx context.Context, o Owner, characterID string) (*voiceref.Reference, error) {
	if err := s.exists(ctx, o); err != nil {
		return nil, err
	}
	return s.current(ctx, o, characterID)
}

// WeightFiles are the absolute paths of the two weight files of a trained voice ("" for both when there is no assets directory).
func (s *Service) WeightFiles(ft voiceref.Finetune) (gpt, sovits string) {
	return s.path(ft.GPTPath()), s.path(ft.SoVITSPath())
}

// checkWeights is nil when both files of the trained voice are there at the size they were written with.
func (s *Service) checkWeights(ft voiceref.Finetune) error {
	gpt, sovits := s.WeightFiles(ft)
	for _, f := range []struct {
		path string
		size int64
	}{{gpt, ft.GPTSizeBytes}, {sovits, ft.SoVITSSizeBytes}} {
		st, err := os.Stat(f.path)
		if f.path == "" || err != nil || !st.Mode().IsRegular() || st.Size() != f.size {
			return fmt.Errorf("%w: %s", ErrNoWeights, ft.ID)
		}
	}
	return nil
}

// BindFinetune makes the weights in "voicetrain/<ft.ID>/" the trained voice of the Character: a canon Character gets a new version of the
// canon (the Episodes that exist keep what they have). The Character needs a reference voice (the synthesis uses its recording), else
// ErrNoReference; weights that are not where ft says are ErrNoWeights. The weights the Character had before are removed once no
// Character refers to them.
func (s *Service) BindFinetune(ctx context.Context, o Owner, characterID string, ft voiceref.Finetune) error {
	if err := ft.Validate(); err != nil {
		return err
	}
	if err := s.checkWeights(ft); err != nil {
		return err
	}
	have, err := s.Reference(ctx, o, characterID)
	if err != nil {
		return err
	}
	if have == nil {
		return ErrNoReference
	}
	ref := *have
	ref.Finetune = &ft
	previous, err := s.write(ctx, o, characterID, &ref)
	if err != nil {
		return err
	}
	s.discard(ctx, previous, &ref)
	return nil
}

// RemoveFinetune takes the trained voice off the Character: it speaks with the zero-shot clone of its recording again. A Character that
// has none is left as it is.
func (s *Service) RemoveFinetune(ctx context.Context, o Owner, characterID string) error {
	have, err := s.Reference(ctx, o, characterID)
	if err != nil || have == nil || have.Finetune == nil {
		return err
	}
	ref := *have
	ref.Finetune = nil
	previous, err := s.write(ctx, o, characterID, &ref)
	if err != nil {
		return err
	}
	s.discard(ctx, previous, &ref)
	return nil
}

// discardWeights removes the directory of the trained voice of old once no Character refers to it. keep is the reference that was just
// set: its weights stay.
func (s *Service) discardWeights(ctx context.Context, old, keep *voiceref.Reference) {
	if old == nil || old.Finetune == nil || (keep != nil && keep.Finetune != nil && keep.Finetune.ID == old.Finetune.ID) {
		return
	}
	s.removeWeightsUnlessUsed(context.WithoutCancel(ctx), old.Finetune.ID)
}

func (s *Service) removeWeightsUnlessUsed(ctx context.Context, id string) {
	if !voiceref.ValidFinetuneID(id) {
		return
	}
	if used, err := s.repo.FinetuneInUse(ctx, id); err != nil || used {
		if err != nil {
			s.logger.Error("check the trained voice failed", "id", id, "err", err)
		}
		return
	}
	if dir := s.path(voiceref.Finetune{ID: id}.Dir()); dir != "" {
		if err := os.RemoveAll(dir); err != nil {
			s.logger.Error("remove the trained voice failed", "id", id, "err", err)
		}
	}
}

// CleanFinetunes removes the directories of trained voices that no Character refers to: what a stop in the middle of binding a trained voice left.
// It runs at startup, when no training is running.
func (s *Service) CleanFinetunes(ctx context.Context) {
	root := s.WeightsRoot()
	if root == "" {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.logger.Error("list the trained voices failed", "err", err)
		}
		return
	}
	for _, e := range entries {
		switch {
		case !e.IsDir():
		case voiceref.ValidFinetuneID(e.Name()):
			s.removeWeightsUnlessUsed(ctx, e.Name())
		case strings.HasPrefix(e.Name(), StagingPrefix): // the copy of weights that a stop in the middle of the keep left
			if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
				s.logger.Error("remove the unfinished weights failed", "dir", e.Name(), "err", err)
			}
		}
	}
}

// StagingPrefix starts the name of the directory the weights are copied into before they are moved to their own: the training service writes
// it, and CleanFinetunes removes what an unfinished copy left.
const StagingPrefix = ".tmp-"

// WeightsDir is the absolute directory of a trained voice ("" for an id that is no directory name): the training service writes it.
func (s *Service) WeightsDir(id string) string {
	if !voiceref.ValidFinetuneID(id) {
		return ""
	}
	return filepath.Join(s.WeightsRoot(), id)
}
