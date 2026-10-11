package judge

import (
	"context"
	"fmt"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// input gathers what the Judge needs: the spec the Take was generated from (with its Reference
// Pack), the neighbouring Shots, the LOCKed parameters and the face references of the Character.
func (r *Runner) input(ctx context.Context, ref repository.TakeRef) (Input, error) {
	snapshot := domain.Shot{ShotSpec: ref.SpecSnapshot}
	spec, ok := snapshot.CanonicalSpec()
	if !ok {
		return Input{}, fmt.Errorf("take %s: its prompt version has no Canonical Shot Spec", ref.Take.ID)
	}
	path, err := r.videoPath(ref.Take)
	if err != nil {
		return Input{}, err
	}
	in := Input{VideoPath: path, Spec: spec}
	if r.Policies != nil {
		policy, err := r.Policies.Get(ctx, ref.ProjectID)
		if err != nil {
			return Input{}, err
		}
		in.Template = policy.Template
	}
	if r.Brands != nil {
		brand, err := r.Brands.List(ctx, ref.ProjectID)
		if err != nil {
			return Input{}, err
		}
		in.Brand = brand
	}

	shots, err := r.Generations.StoryShots(ctx, ref.ProjectID, ref.BranchID)
	if err != nil {
		return Input{}, err
	}
	in.Previous, in.Next = neighbours(shots, ref.Take.ShotID)

	references := snapshot.References()
	for _, rf := range references {
		if img, ok := r.image(ctx, ref.ProjectID, rf.AssetID); ok {
			in.References = append(in.References, RefImage{Kind: rf.Kind, Name: rf.Name, MimeType: img.mime, Data: img.data})
		}
	}

	locks, err := r.Judges.Locks(ctx, ref.ProjectID)
	if err != nil {
		return Input{}, err
	}
	world, err := r.Generations.WorldContext(ctx, ref.ProjectID, ref.BranchID)
	if err != nil {
		return Input{}, err
	}
	shotLocks, err := r.Generations.ShotLocks(ctx, ref.Take.ShotID)
	if err != nil {
		return Input{}, err
	}
	in.Locked = lockedDimensions(spec, world, locks, shotLocks)

	if spec.Character != nil {
		if subject, ok := r.faceSubject(ctx, ref.ProjectID, *spec.Character, world, references); ok {
			in.FaceSubjects = []FaceSubject{subject}
		}
	}
	return in, nil
}

// neighbours returns the specs of the Shots before and after shotID in story order.
func neighbours(shots []repository.ShotRef, shotID string) (prev, next *domain.ShotSpec) {
	for i, s := range shots {
		if s.Shot.ID != shotID {
			continue
		}
		if i > 0 {
			if spec, ok := shots[i-1].Shot.CanonicalSpec(); ok {
				prev = &spec
			}
		}
		if i+1 < len(shots) {
			if spec, ok := shots[i+1].Shot.CanonicalSpec(); ok {
				next = &spec
			}
		}
		return prev, next
	}
	return nil, nil
}

// dimensionsOfLock maps the LOCKed attributes of a Character, Product or Location to the Judge
// dimensions that check them (spec/spec.md §9.3): a deviation in such a dimension is a LOCK
// violation and fails the Take whatever the score.
var dimensionsOfLock = map[domain.WorldTarget]map[string][]domain.JudgeDimension{
	domain.TargetCharacter: {
		domain.LockParamIdentity: {domain.DimCharacterIdentity, domain.DimFace},
		"wardrobe":               {domain.DimCharacterConsistency},
		"hairstyle":              {domain.DimCharacterConsistency},
	},
	domain.TargetProduct: {
		domain.LockParamGeometry: {domain.DimProductGeometry},
		domain.LockParamLabel:    {domain.DimLogoTextAccuracy},
		"orientation":            {domain.DimProductAccuracy},
		"material":               {domain.DimProductAccuracy},
		"condition":              {domain.DimProductAccuracy},
	},
	domain.TargetLocation: {
		"time":                          {domain.DimLighting},
		"lighting_state":                {domain.DimLighting},
		"weather":                       {domain.DimBackground},
		domain.LockFieldLayoutReference: {domain.DimBackground},
	},
}

// dimensionsOfShotLock maps the parameters the user LOCKed on the Shot itself (PUT /shots/:id/locks,
// spec/spec.md §6) to the Judge dimensions that check them.
var dimensionsOfShotLock = map[domain.LockParameter][]domain.JudgeDimension{
	domain.ParamCharacterIdentity: {domain.DimCharacterIdentity, domain.DimFace},
	domain.ParamProductGeometry:   {domain.DimProductGeometry},
	domain.ParamActing:            {domain.DimActing},
	domain.ParamCamera:            {domain.DimCameraMotion},
	domain.ParamLighting:          {domain.DimLighting},
}

// lockedDimensions maps the LOCKs that apply to the Shot to Judge dimensions. The Character,
// Product and Location of the Shot are found in the World State by ID or name (the Shot Spec
// uses either); an attribute is LOCKed by a lock row of that entity, and Character Identity and
// Product Geometry also by the Shot Spec itself ("locked"). The parameters LOCKed on the Shot
// (shotLocks: Character Identity, Product Geometry, Acting, Camera, Lighting) count as well.
func lockedDimensions(spec domain.ShotSpec, world domain.WorldContext, locks domain.LockSet, shotLocks domain.ParameterLocks) map[domain.JudgeDimension]bool {
	out := map[domain.JudgeDimension]bool{}
	for param, dims := range dimensionsOfShotLock {
		if shotLocks.Locked(param) {
			for _, d := range dims {
				out[d] = true
			}
		}
	}
	mark := func(target domain.WorldTarget, id string) {
		for field, dims := range dimensionsOfLock[target] {
			if locks.Locked(target, id, field) {
				for _, d := range dims {
					out[d] = true
				}
			}
		}
	}
	if c := spec.Character; c != nil {
		id := c.ID
		if w, ok := world.Character(c.ID); ok {
			id = w.ID
		}
		mark(domain.TargetCharacter, id)
		if c.Identity == "locked" {
			out[domain.DimCharacterIdentity], out[domain.DimFace] = true, true
		}
	}
	if p := spec.Product; p != nil {
		id := p.ID
		if w, ok := world.Product(p.ID); ok {
			id = w.ID
		}
		mark(domain.TargetProduct, id)
		if p.Geometry == "locked" {
			out[domain.DimProductGeometry] = true
		}
	}
	if l := spec.Location; l != nil {
		id := l.ID
		if w, ok := world.Location(l.ID); ok {
			id = w.ID
		}
		mark(domain.TargetLocation, id)
	}
	return out
}

// faceSubject is the Character of the Shot with its reference faces: the face_reference of the
// Series canon the Character was copied from (so that the face is measured against the same
// baseline in every Episode, F-29), the Character's own face_reference asset and the Character
// images of the Reference Pack that carry its name. Images that are the same file count once: an
// Episode's copy of the canon face must not weigh twice in the calibration of the thresholds.
func (r *Runner) faceSubject(ctx context.Context, projectID string, actor domain.SpecActor, world domain.WorldContext, refs []domain.Reference) (FaceSubject, bool) {
	// The Shot Spec names the Character by ID or by name, whichever the Prompt Planner wrote.
	var character *domain.WorldCharacter
	for i, c := range world.Characters {
		if c.ID == actor.ID || strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(actor.ID)) {
			character = &world.Characters[i]
			break
		}
	}
	subject := FaceSubject{ID: actor.ID}
	if character == nil {
		return subject, false
	}
	seen := map[string]bool{}
	add := func(img storedImage, ok bool) {
		if ok && !seen[img.path] {
			seen[img.path] = true
			subject.ReferenceImages = append(subject.ReferenceImages, img.data)
		}
	}
	if id := character.SeriesFaceReferenceAssetID; id != "" {
		add(r.canonImage(ctx, id))
	}
	if id := character.FaceReferenceAssetID; id != "" {
		add(r.image(ctx, projectID, id))
	}
	for _, rf := range refs {
		if rf.Kind == domain.RefCharacter && strings.EqualFold(strings.TrimSpace(rf.Name), strings.TrimSpace(character.Name)) {
			add(r.image(ctx, projectID, rf.AssetID))
		}
	}
	return subject, len(subject.ReferenceImages) > 0
}
