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

// madeReference is one planned image of the Reference Pack once it has been resolved.
type madeReference struct {
	plan   artdirector.Plan
	output modeladapter.ImageOutput
	asset  domain.Asset
	// reused is set when the image already exists (a pinned Library image or the image of a
	// World State reference slot): nothing was generated, so nothing is paid or stored.
	reused  bool
	pinning domain.Reference // the reference kept for a reused image
}

// packBuild is the state of one buildReferencePack run: the design and what each plan became.
// results has one slot per plan; images made at the same time write distinct slots.
type packBuild struct {
	ref      repository.ShotRef
	plans    []artdirector.Plan
	world    domain.WorldContext
	uploaded []modeladapter.Input
	ratio    string
	pinned   []domain.Reference
	results  []madeReference
}

// buildReferencePack has the Art Director design the Reference Pack of the Shot and makes the
// images (F-11). Character / Product / Location / Style / Lighting References come first; the
// Keyframe is made last, from those references, so that it shows the same character and product.
// The old Reference Pack is replaced only when every image was made.
func (o *Orchestrator) buildReferencePack(ctx context.Context, ref repository.ShotRef) (Progress, error) {
	in, err := o.designInput(ctx, ref)
	if err != nil {
		return Progress{}, err
	}
	plans, err := o.art.Design(ctx, in)
	if err != nil {
		return Progress{}, err
	}
	b := &packBuild{
		ref: ref, plans: plans, world: in.World,
		uploaded: o.loadInputs(ctx, ref.ProjectID, in.Brief.Assets, maxUploadedInputs),
		ratio:    aspectRatio(in.Brief),
		pinned:   pinnedReferences(ref.Shot),
		results:  make([]madeReference, len(plans)),
	}
	for i, p := range plans {
		if p.Kind == domain.RefKeyframe {
			continue
		}
		if r, asset, ok := o.reusable(ctx, ref.ProjectID, p, b.pinned, b.world); ok {
			b.results[i] = madeReference{plan: p, asset: asset, reused: true, pinning: r}
		}
	}

	if failed := o.generateImages(ctx, b); len(failed) > 0 {
		o.removeMade(b)
		return Progress{}, errors.Join(failed...)
	}

	pack := b.assemblePack()
	if err := o.gens.SaveReferencePack(context.WithoutCancel(ctx), pack); err != nil {
		for _, a := range pack.Assets {
			o.files.remove(a)
		}
		return Progress{}, err
	}
	o.announceKeyframes(ctx, b)
	return Progress{TakesDone: len(plans), TakesTotal: len(plans)}, nil
}

// designInput collects what the Art Director designs the Reference Pack from.
func (o *Orchestrator) designInput(ctx context.Context, ref repository.ShotRef) (artdirector.Input, error) {
	project, err := o.projects.Get(ctx, ref.ProjectID)
	if err != nil {
		return artdirector.Input{}, err
	}
	brief, err := o.projects.GetBrief(ctx, ref.ProjectID)
	if err != nil {
		return artdirector.Input{}, err
	}
	world, err := o.gens.WorldContext(ctx, ref.ProjectID, ref.Scene.BranchID)
	if err != nil {
		return artdirector.Input{}, err
	}
	in := artdirector.Input{
		Brief: brief, Template: project.ProductionTemplate, Scene: ref.Scene, Shot: ref.Shot, World: world,
		UploadedImages: len(brief.Assets),
	}
	if spec, ok := ref.Shot.CanonicalSpec(); ok {
		in.Spec = &spec
	}
	return in, nil
}

// generateImages makes the images that do not exist yet and returns the errors of those that
// failed. References are made first, at the same time; then the Keyframe with them as input. An
// image that exists already is not made again but still goes to the Keyframe.
func (o *Orchestrator) generateImages(ctx context.Context, b *packBuild) []error {
	var first, keyframes, existing []int
	for i, p := range b.plans {
		switch {
		case p.Kind == domain.RefKeyframe:
			keyframes = append(keyframes, i)
		case b.results[i].reused:
			existing = append(existing, i)
		default:
			first = append(first, i)
		}
	}
	shotID := b.ref.Shot.ID
	o.notify(Progress{ShotID: shotID, Action: ActionReferencePack, Stage: StageGenerating, Message: "参照画像を生成しています", TakesTotal: len(b.plans)})
	errs := parallel(o.logger, len(first), func(k int) error { return o.generateOne(ctx, b, first[k], nil) })
	var failed []error
	for _, e := range errs {
		if e != nil {
			failed = append(failed, e)
		}
	}
	if len(failed) > 0 {
		return failed
	}
	var inputs []modeladapter.Input
	for _, i := range append(slices.Clone(existing), first...) {
		if k := b.plans[i].Kind; k == domain.RefCharacter || k == domain.RefProduct || k == domain.RefLocation {
			if data, err := o.files.read(b.results[i].asset); err == nil {
				inputs = append(inputs, modeladapter.Input{Data: data, MimeType: b.results[i].asset.MimeType})
			}
		}
	}
	o.notify(Progress{ShotID: shotID, Action: ActionReferencePack, Stage: StageGenerating, Message: "Keyframe を生成しています", TakesDone: len(first), TakesTotal: len(b.plans)})
	for _, i := range keyframes {
		if err := o.generateOne(ctx, b, i, inputs); err != nil {
			failed = append(failed, err)
		}
	}
	return failed
}

// generateOne makes the image of plan i: reserves its estimate, generates it, puts it on the
// ledger and stores the file. extra are further model inputs (the references of the Keyframe).
func (o *Orchestrator) generateOne(ctx context.Context, b *packBuild, i int, extra []modeladapter.Input) error {
	p := b.plans[i]
	projectID, shotID := b.ref.ProjectID, b.ref.Shot.ID
	var inputs []modeladapter.Input
	if p.UseUploaded {
		inputs = append(inputs, b.uploaded...)
	}
	inputs = append(inputs, o.worldInputs(ctx, projectID, p.Kind, b.world)...)
	inputs = append(inputs, extra...)
	purpose := purposeOf(p, len(inputs) > 0)
	// The estimate is held until the image is on the ledger, so images that run at the same time
	// (here and in other jobs) cannot together pass the budget ceilings.
	release, ok, err := o.gens.Reserve(ctx, projectID, shotID, o.adapter.EstimateImage(purpose))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s: %w", p.Kind, ErrBudgetExceeded)
	}
	defer release()
	out, err := o.adapter.GenerateImage(ctx, purpose, modeladapter.ImageRequest{Prompt: p.Prompt, AspectRatio: b.ratio, Inputs: inputs})
	if err != nil {
		return fmt.Errorf("%s: %w", p.Kind, err)
	}
	// The provider has charged for the image: the ledger gets it before a later failure (this
	// image's file, a sibling image, the Keyframe or the Pack) can lose it.
	o.recordPaid(ctx, projectID, shotID, out.Model, out.EstimatedCost, out.ActualCost)
	asset, err := o.files.write(projectID, domain.AssetReference, out.MimeType, out.Data)
	if err != nil {
		return err
	}
	b.results[i] = madeReference{plan: p, output: out, asset: asset}
	return nil
}

// removeMade deletes the files of the images this run generated (a reused image is not ours).
func (o *Orchestrator) removeMade(b *packBuild) {
	for _, r := range b.results {
		if r.asset.ID != "" && !r.reused {
			o.files.remove(r.asset)
		}
	}
}

// assemblePack builds the new Reference Pack from the resolved images. A pinned image the Art
// Director did not plan stays in the Pack.
func (b *packBuild) assemblePack() repository.NewReferencePack {
	pack := repository.NewReferencePack{ProjectID: b.ref.ProjectID, ShotID: b.ref.Shot.ID}
	for _, r := range b.results {
		if r.reused {
			pack.References = append(pack.References, r.pinning)
			continue
		}
		pack.References = append(pack.References, domain.Reference{
			Kind: r.plan.Kind, Name: r.plan.Name, AssetID: r.asset.ID, Prompt: r.plan.Prompt, Model: r.output.Model,
		})
		pack.Assets = append(pack.Assets, r.asset)
	}
	for _, p := range b.pinned {
		if !slices.ContainsFunc(b.plans, func(pl artdirector.Plan) bool { return matchesPlan(p, pl) }) {
			pack.References = append(pack.References, p)
		}
	}
	return pack
}

// announceKeyframes posts a card to the chat for each image this run generated.
func (o *Orchestrator) announceKeyframes(ctx context.Context, b *packBuild) {
	for _, r := range b.results {
		if r.reused {
			continue
		}
		label := kindLabel[r.plan.Kind]
		if r.plan.Name != "" {
			label += "（" + r.plan.Name + "）"
		}
		card := domain.ChatMessage{
			Role: domain.RoleAssistant, Kind: domain.KindKeyframe, RefID: r.asset.ID,
			Content: fmt.Sprintf("Shot %d: %s", b.ref.Shot.OrderIndex+1, label),
		}
		if err := o.chat.Announce(context.WithoutCancel(ctx), b.ref.ProjectID, card); err != nil {
			o.logger.Error("post keyframe card failed", "asset_id", r.asset.ID, "err", err)
		}
	}
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

// matchesPlan reports whether the pinned reference takes the place of the planned image
// (domain.SameReference: the same kind and, for Character / Product / Location, the same name).
func matchesPlan(r domain.Reference, p artdirector.Plan) bool {
	return domain.SameReference(r, domain.Reference{Kind: p.Kind, Name: p.Name})
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
