package generation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ousiassllc/mam/backend/internal/agent/artdirector"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/repository"
)

// maxUploadedInputs bounds how many uploaded images go to a model with one reference.
const maxUploadedInputs = 4

// kindLabel names a reference kind in the cards of the chat.
var kindLabel = map[domain.ReferenceKind]string{
	domain.RefCharacter: "Character Reference",
	domain.RefProduct:   "Product Reference",
	domain.RefLocation:  "Location Reference",
	domain.RefStyle:     "Style Reference",
	domain.RefLighting:  "Lighting Reference",
	domain.RefKeyframe:  "Keyframe",
}

// buildReferencePack has the Art Director design the Reference Pack of the Shot and makes the
// images (F-11). Character / Product / Location / Style / Lighting References come first; the
// Keyframe is made last, from those references, so that it shows the same character and product.
// The old Reference Pack is replaced only when every image was made.
func (o *Orchestrator) buildReferencePack(ctx context.Context, ref repository.ShotRef) (Progress, error) {
	shot := ref.Shot
	project, err := o.projects.Get(ctx, ref.ProjectID)
	if err != nil {
		return Progress{}, err
	}
	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return Progress{}, err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID)
	if err != nil {
		return Progress{}, err
	}
	in := artdirector.Input{
		Brief: brief, Template: project.ProductionTemplate, Scene: ref.Scene, Shot: shot, World: world,
		UploadedImages: len(brief.Assets),
	}
	if spec, ok := shot.CanonicalSpec(); ok {
		in.Spec = &spec
	}
	plans, err := o.art.Design(ctx, in)
	if err != nil {
		return Progress{}, err
	}
	uploaded := o.loadInputs(ctx, ref.ProjectID, brief.Assets, maxUploadedInputs)
	ratio := aspectRatio(brief)

	type made struct {
		plan   artdirector.Plan
		output modeladapter.ImageOutput
		asset  domain.Asset
		// reused is set when the image already exists (a pinned Library image or the image of a
		// World State reference slot): nothing was generated, so nothing is paid or stored.
		reused  bool
		pinning domain.Reference // the reference kept for a reused image
	}
	results := make([]made, len(plans))
	pinned := pinnedReferences(shot)
	for i, p := range plans {
		if p.Kind == domain.RefKeyframe {
			continue
		}
		if r, asset, ok := o.reusable(ctx, ref.ProjectID, p, pinned, world); ok {
			results[i] = made{plan: p, asset: asset, reused: true, pinning: r}
		}
	}
	generate := func(i int, extra []modeladapter.Input) error {
		p := plans[i]
		var inputs []modeladapter.Input
		if p.UseUploaded {
			inputs = append(inputs, uploaded...)
		}
		inputs = append(inputs, o.worldInputs(ctx, ref.ProjectID, p.Kind, world)...)
		inputs = append(inputs, extra...)
		out, err := o.adapter.GenerateImage(ctx, purposeOf(p, len(inputs) > 0), modeladapter.ImageRequest{Prompt: p.Prompt, AspectRatio: ratio, Inputs: inputs})
		if err != nil {
			return fmt.Errorf("%s: %w", p.Kind, err)
		}
		asset, err := o.files.write(ref.ProjectID, domain.AssetReference, out.MimeType, out.Data)
		if err != nil {
			return err
		}
		results[i] = made{plan: p, output: out, asset: asset}
		return nil
	}

	// References first, at the same time; then the Keyframe with them as input. An image that
	// exists already is not made again but still goes to the Keyframe.
	var first, keyframes, existing []int
	for i, p := range plans {
		switch {
		case p.Kind == domain.RefKeyframe:
			keyframes = append(keyframes, i)
		case results[i].reused:
			existing = append(existing, i)
		default:
			first = append(first, i)
		}
	}
	o.notify(Progress{ShotID: shot.ID, Action: ActionReferencePack, Stage: StageGenerating, Message: "参照画像を生成しています", TakesTotal: len(plans)})
	errs := parallel(len(first), func(k int) error { return generate(first[k], nil) })
	var failed []error
	for _, e := range errs {
		if e != nil {
			failed = append(failed, e)
		}
	}
	if len(failed) == 0 {
		var inputs []modeladapter.Input
		for _, i := range append(slices.Clone(existing), first...) {
			if k := plans[i].Kind; k == domain.RefCharacter || k == domain.RefProduct || k == domain.RefLocation {
				if data, err := o.files.read(results[i].asset); err == nil {
					inputs = append(inputs, modeladapter.Input{Data: data, MimeType: results[i].asset.MimeType})
				}
			}
		}
		o.notify(Progress{ShotID: shot.ID, Action: ActionReferencePack, Stage: StageGenerating, Message: "Keyframe を生成しています", TakesDone: len(first), TakesTotal: len(plans)})
		for _, i := range keyframes {
			if err := generate(i, inputs); err != nil {
				failed = append(failed, err)
			}
		}
	}
	if len(failed) > 0 {
		removeMade := func() {
			for _, r := range results {
				if r.asset.ID != "" && !r.reused {
					o.files.remove(r.asset)
				}
			}
		}
		removeMade()
		return Progress{}, errors.Join(failed...)
	}

	pack := repository.NewReferencePack{ProjectID: ref.ProjectID, ShotID: shot.ID}
	for _, r := range results {
		if r.reused {
			pack.References = append(pack.References, r.pinning)
			continue
		}
		pack.References = append(pack.References, domain.Reference{
			Kind: r.plan.Kind, Name: r.plan.Name, AssetID: r.asset.ID, Prompt: r.plan.Prompt, Model: r.output.Model,
		})
		pack.Assets = append(pack.Assets, r.asset)
		pack.Costs = append(pack.Costs, domain.CostEntry{
			ProjectID: ref.ProjectID, ShotID: shot.ID, Provider: r.output.Model, EstimatedCost: r.output.EstimatedCost, ActualCost: r.output.ActualCost,
		})
	}
	// A pinned image the Art Director did not plan stays in the Pack.
	for _, p := range pinned {
		if !slices.ContainsFunc(plans, func(pl artdirector.Plan) bool { return matchesPlan(p, pl) }) {
			pack.References = append(pack.References, p)
		}
	}
	if err := o.gens.SaveReferencePack(context.WithoutCancel(ctx), pack); err != nil {
		for _, a := range pack.Assets {
			o.files.remove(a)
		}
		return Progress{}, err
	}
	for _, r := range results {
		if r.reused {
			continue
		}
		label := kindLabel[r.plan.Kind]
		if r.plan.Name != "" {
			label += "（" + r.plan.Name + "）"
		}
		card := domain.ChatMessage{
			Role: domain.RoleAssistant, Kind: domain.KindKeyframe, RefID: r.asset.ID,
			Content: fmt.Sprintf("Shot %d: %s", shot.OrderIndex+1, label),
		}
		if err := o.chat.Announce(context.WithoutCancel(ctx), ref.ProjectID, card); err != nil {
			o.logger.Error("post keyframe card failed", "asset_id", r.asset.ID, "err", err)
		}
	}
	return Progress{TakesDone: len(plans), TakesTotal: len(plans)}, nil
}

// purposeOf picks the image model row of model-adapters.md §3: material the user provided and
// Style / Lighting / Keyframe images are edits and variations (Nano Banana); new Character /
// Product / Location references need accurate rendering (Imagen 4, or GPT Image 2).
func purposeOf(p artdirector.Plan, hasInputs bool) modeladapter.ImagePurpose {
	switch p.Kind {
	case domain.RefStyle, domain.RefLighting, domain.RefKeyframe:
		return modeladapter.PurposeEditReference
	}
	if hasInputs {
		return modeladapter.PurposeEditReference
	}
	return modeladapter.PurposeNewReference
}

// loadInputs reads up to limit image assets of the Project as model inputs. Missing files are skipped.
func (o *Orchestrator) loadInputs(ctx context.Context, projectID string, ids []string, limit int) []modeladapter.Input {
	var out []modeladapter.Input
	for _, id := range ids {
		if len(out) == limit {
			break
		}
		a, err := o.assets.Get(ctx, projectID, id)
		if err != nil {
			o.logger.Warn("input image unavailable", "asset_id", id, "err", err)
			continue
		}
		data, err := o.files.read(a)
		if err != nil {
			o.logger.Warn("input image unavailable", "asset_id", id, "err", err)
			continue
		}
		out = append(out, modeladapter.Input{Data: data, MimeType: a.MimeType})
	}
	return out
}

// worldInputs are the reference images the World State already holds for the kind (a character's
// face and body, a product's shape and label, a location's layout), so the new reference stays
// consistent with them (F-11, World State continuity).
func (o *Orchestrator) worldInputs(ctx context.Context, projectID string, kind domain.ReferenceKind, w domain.WorldContext) []modeladapter.Input {
	var ids []string
	switch kind {
	case domain.RefCharacter:
		for _, c := range w.Characters {
			ids = append(ids, c.FaceReferenceAssetID, c.BodyReferenceAssetID)
		}
	case domain.RefProduct:
		for _, p := range w.Products {
			ids = append(ids, p.GeometryReferenceAssetID, p.LabelReferenceAssetID)
		}
	case domain.RefLocation:
		for _, l := range w.Locations {
			ids = append(ids, l.LayoutReferenceAssetID)
		}
	}
	var present []string
	for _, id := range ids {
		if id != "" {
			present = append(present, id)
		}
	}
	return o.loadInputs(ctx, projectID, present, maxUploadedInputs)
}

// pinnedReferences are the references of the Shot's current Reference Pack that were taken from
// the Reference Library: a rebuild keeps them (spec/spec.md §10.1).
func pinnedReferences(shot domain.Shot) []domain.Reference {
	var out []domain.Reference
	for _, r := range shot.References() {
		if r.LibraryItem != "" {
			out = append(out, r)
		}
	}
	return out
}

// matchesPlan reports whether the pinned reference takes the place of the planned image: the same
// kind and, for Character / Product / Location, the same name (a Pack has one Style and one
// Lighting image, so those match by kind).
func matchesPlan(r domain.Reference, p artdirector.Plan) bool {
	if r.Kind != p.Kind {
		return false
	}
	return r.Kind == domain.RefStyle || r.Kind == domain.RefLighting || domain.SameReferenceName(r.Name, p.Name)
}

// reusable returns the image that already exists for the planned reference, so that it is not
// generated again: the image pinned to the Shot from the Reference Library, else the image the World
// State holds for the named Character (face), Product (geometry) or Location (layout).
func (o *Orchestrator) reusable(ctx context.Context, projectID string, p artdirector.Plan, pinned []domain.Reference, w domain.WorldContext) (domain.Reference, domain.Asset, bool) {
	for _, r := range pinned {
		if !matchesPlan(r, p) {
			continue
		}
		if a, err := o.assets.Get(ctx, projectID, r.AssetID); err == nil {
			return r, a, true
		}
	}
	var assetID string
	switch p.Kind {
	case domain.RefCharacter:
		if c, ok := w.Character(p.Name); ok {
			assetID = c.FaceReferenceAssetID
		}
	case domain.RefProduct:
		if pr, ok := w.Product(p.Name); ok {
			assetID = pr.GeometryReferenceAssetID
		}
	case domain.RefLocation:
		if l, ok := w.Location(p.Name); ok {
			assetID = l.LayoutReferenceAssetID
		}
	}
	if assetID == "" {
		return domain.Reference{}, domain.Asset{}, false
	}
	a, err := o.assets.Get(ctx, projectID, assetID)
	if err != nil {
		return domain.Reference{}, domain.Asset{}, false
	}
	return domain.Reference{Kind: p.Kind, Name: p.Name, AssetID: a.ID, Prompt: p.Prompt}, a, true
}
