package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Inspector is the Shot Inspector (GET /shots/:id): the Shot, its Canonical Shot Spec, Take list
// and the AUTO / MANUAL / LOCK setting of each parameter.
type Inspector struct {
	Project domain.Project
	Scene   domain.Scene
	Shot    domain.Shot
	Spec    *domain.ShotSpec // nil until a spec exists
	Takes   []domain.Take
	Locks   domain.ParameterLocks
	// Pack is the Reference Pack of the Shot and Library the images of the Project that can be added to it (F-11).
	Pack    []domain.Reference
	Library []domain.ReferenceItem
	// SpecInput and SpecError hold a rejected manual edit of the spec.
	SpecInput string
	SpecError string
	// Versions are the saved versions of the Shot's spec, newest first (F-28).
	Versions []domain.VersionEntry
	// Judged is the latest Judge result of each Take that was judged, by Take ID.
	Judged map[string]domain.JudgeResult
}

// SpecText is the Canonical Shot Spec as the edit form shows it (indented JSON).
func (i Inspector) SpecText() string {
	if i.SpecInput != "" {
		return i.SpecInput
	}
	if i.Spec == nil {
		return ""
	}
	b, err := json.MarshalIndent(i.Spec, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// Inspector returns the Shot Inspector.
func (s *DirectorService) Inspector(ctx context.Context, shotID string) (Inspector, error) {
	ref, err := s.gens.GetShot(ctx, shotID)
	if err != nil {
		return Inspector{}, notFound(err)
	}
	project, err := s.getProject(ctx, ref.ProjectID)
	if err != nil {
		return Inspector{}, err
	}
	takes, err := s.plans.ListTakes(ctx, ref.Scene.ID)
	if err != nil {
		return Inspector{}, err
	}
	locks, err := s.locks.ShotLocks(ctx, shotID)
	if err != nil {
		return Inspector{}, err
	}
	library, err := s.references.List(ctx, ref.ProjectID, repository.ReferenceFilter{})
	if err != nil {
		return Inspector{}, err
	}
	versions, err := s.versions.ShotVersions(ctx, shotID)
	if err != nil {
		return Inspector{}, notFound(err)
	}
	view := Inspector{Project: project, Scene: ref.Scene, Shot: ref.Shot, Takes: takes[shotID], Locks: locks, Pack: ref.Shot.References(), Library: library, Versions: versions, Judged: map[string]domain.JudgeResult{}}
	for _, t := range view.Takes {
		res, err := s.judges.Latest(ctx, t.ID)
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return Inspector{}, err
		}
		view.Judged[t.ID] = res
	}
	if spec, ok := ref.Shot.CanonicalSpec(); ok {
		view.Spec = &spec
	}
	return view, nil
}

// EditSpec stores a Canonical Shot Spec that the user edited by hand (PUT /shots/:id/shot-spec).
// text is the spec as JSON (the Inspector's form). A spec that does not parse or fails
// domain.ShotSpec.Validate is a ValidationError (the returned Inspector keeps the input and the
// reason); a change to a LOCKed parameter is ErrLocked and writes nothing; a Shot being generated is ErrBusy.
func (s *DirectorService) EditSpec(ctx context.Context, shotID, text string) (Inspector, error) {
	ref, err := s.gens.GetShot(ctx, shotID)
	if err != nil {
		return Inspector{}, notFound(err)
	}
	reject := func(msg string) (Inspector, error) {
		view, err := s.Inspector(ctx, shotID)
		if err != nil {
			return Inspector{}, err
		}
		view.SpecInput, view.SpecError = text, msg
		return view, invalid("shot_spec", "%s", msg)
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.DisallowUnknownFields()
	var spec domain.ShotSpec
	if err := dec.Decode(&spec); err != nil {
		return reject("Shot Spec を JSON として読み取れません: " + err.Error())
	}
	spec.Duration = domain.FormatDuration(ref.Shot.Duration)
	if err := spec.Validate(ref.Shot.Duration); err != nil {
		return reject("Shot Spec が正しくありません: " + err.Error())
	}
	if _, err := s.gens.SaveEditedSpec(ctx, shotID, spec); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return Inspector{}, ErrBusy
		}
		return Inspector{}, lockedError(notFound(err))
	}
	s.recordIntervention(ctx, domain.Intervention{ShotID: shotID, Kind: domain.InterventionEditShotSpec, Override: true})
	return s.Inspector(ctx, shotID)
}
