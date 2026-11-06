// Package refimage reads the images of the References of a Shot once for a run (a job, or the plan before it): the strategy plans
// the clip from the images that can be read (a Luma Ray 3.2 clip is 5 seconds long only when its Keyframe is read, #902) and the
// clip is made from the same reads, so no image is read twice and the plan is the clip that is made (#1062).
package refimage

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// Assets finds the asset row of a Reference. *repository.AssetRepository implements it.
type Assets interface {
	Get(ctx context.Context, projectID, id string) (domain.Asset, error)
}

// Reader reads the images of one Project's References, each at most once. It is safe for the Takes of a run that go at the same time.
type Reader struct {
	projectID string
	assets    Assets
	read      func(domain.Asset) ([]byte, error)
	logger    *slog.Logger

	mu   sync.Mutex
	seen map[string]entry // by asset ID
}

// entry is what reading one image gave: ok is false when the image is not there to read.
type entry struct {
	in modeladapter.Input
	ok bool
}

// New is a Reader of the Project whose assets are found by assets and whose files are read by read.
func New(projectID string, assets Assets, read func(domain.Asset) ([]byte, error), logger *slog.Logger) *Reader {
	return &Reader{projectID: projectID, assets: assets, read: read, logger: logger, seen: map[string]entry{}}
}

// Input is the image of the Reference as a model input. ok is false when the image is not there to read: the Reference Pack can be
// rebuilt, so a missing or unreadable image is not fatal. A database error is returned, and a later call asks again.
func (r *Reader) Input(ctx context.Context, ref domain.Reference) (in modeladapter.Input, ok bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, done := r.seen[ref.AssetID]; done {
		return e.in, e.ok, nil
	}
	asset, err := r.assets.Get(ctx, r.projectID, ref.AssetID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return in, false, err
	}
	var e entry
	if err == nil {
		data, readErr := r.read(asset)
		if readErr != nil {
			r.logger.Warn("reference image unavailable", "asset_id", ref.AssetID, "err", readErr)
		} else {
			e = entry{in: modeladapter.Input{Data: data, MimeType: asset.MimeType}, ok: true}
		}
	}
	r.seen[ref.AssetID] = e
	return e.in, e.ok, nil
}

// KeyframeRead reports whether a Keyframe of refs can be read, that is whether the clip gets a first frame.
func (r *Reader) KeyframeRead(ctx context.Context, refs []domain.Reference) (bool, error) {
	for _, ref := range refs {
		if ref.Kind != domain.RefKeyframe {
			continue
		}
		if _, ok, err := r.Input(ctx, ref); ok || err != nil {
			return ok, err
		}
	}
	return false, nil
}

// Attached is refs less its Keyframe when none can be read (the clip then has no first frame): the strategy plans the clip (length,
// cost, routing) from these.
func (r *Reader) Attached(ctx context.Context, refs []domain.Reference) ([]domain.Reference, error) {
	isKeyframe := func(ref domain.Reference) bool { return ref.Kind == domain.RefKeyframe }
	if !slices.ContainsFunc(refs, isKeyframe) {
		return refs, nil
	}
	read, err := r.KeyframeRead(ctx, refs)
	if err != nil || read {
		return refs, err
	}
	return slices.DeleteFunc(slices.Clone(refs), isKeyframe), nil
}

// Inputs reads up to limit of the images of the assets as model inputs, in order. A missing or unreadable file is skipped.
func (r *Reader) Inputs(ctx context.Context, ids []string, limit int) []modeladapter.Input {
	var out []modeladapter.Input
	for _, id := range ids {
		if len(out) == limit {
			break
		}
		if in, ok, err := r.Input(ctx, domain.Reference{AssetID: id}); err != nil {
			r.logger.Warn("input image unavailable", "asset_id", id, "err", err)
		} else if ok {
			out = append(out, in)
		}
	}
	return out
}

// WorldAssetIDs are the reference images the World State already holds for the kind (a character's face and body, a product's
// shape and label, a location's layout), so a new reference stays consistent with them (F-11, World State continuity).
func WorldAssetIDs(kind domain.ReferenceKind, w domain.WorldContext) []string {
	var ids []string
	add := func(id string) {
		if id != "" {
			ids = append(ids, id)
		}
	}
	switch kind {
	case domain.RefCharacter:
		for _, c := range w.Characters {
			add(c.FaceReferenceAssetID)
			add(c.BodyReferenceAssetID)
		}
	case domain.RefProduct:
		for _, p := range w.Products {
			add(p.GeometryReferenceAssetID)
			add(p.LabelReferenceAssetID)
		}
	case domain.RefLocation:
		for _, l := range w.Locations {
			add(l.LayoutReferenceAssetID)
		}
	}
	return ids
}
