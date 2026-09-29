package generation

import (
	"context"
	"errors"
	"fmt"

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
	}
	results := make([]made, len(plans))
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

	// References first, at the same time; then the Keyframe with them as input.
	var first, keyframes []int
	for i, p := range plans {
		if p.Kind == domain.RefKeyframe {
			keyframes = append(keyframes, i)
		} else {
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
		for _, i := range first {
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
		for _, r := range results {
			if r.asset.ID != "" {
				o.files.remove(r.asset)
			}
		}
		return Progress{}, errors.Join(failed...)
	}

	pack := repository.NewReferencePack{ProjectID: ref.ProjectID, ShotID: shot.ID}
	for _, r := range results {
		pack.References = append(pack.References, domain.Reference{
			Kind: r.plan.Kind, Name: r.plan.Name, AssetID: r.asset.ID, Prompt: r.plan.Prompt, Model: r.output.Model,
		})
		pack.Assets = append(pack.Assets, r.asset)
		pack.Costs = append(pack.Costs, domain.CostEntry{
			ProjectID: ref.ProjectID, ShotID: shot.ID, Provider: r.output.Model, EstimatedCost: r.output.EstimatedCost,
		})
	}
	if err := o.gens.SaveReferencePack(context.WithoutCancel(ctx), pack); err != nil {
		for _, r := range results {
			o.files.remove(r.asset)
		}
		return Progress{}, err
	}
	for _, r := range results {
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
