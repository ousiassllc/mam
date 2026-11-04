// Package face holds what the generation test does with faces besides generating them: the swap of a face into a picture by FaceFusion, an
// external tool the user installed (Issue #995, spec/architecture/gentest/face-swap.md), and the adapters the application hands the service
// for the face models (the cut of a "face only" reference, the measurement of the face similarity of a result, #960 / #998). It is a package of
// its own so that package gentest stays under the line limit; it uses the service only through its exported methods and Deps.
package face

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/facefusion"
	"github.com/ousiassllc/mam/backend/internal/fsutil"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

// SwapModel is the model id of a result of "swap the face": FaceFusion is a tool outside mam, not one of the models of the adapter, so
// its results carry this fixed id (the history shows them with it and filters by it like any model).
const SwapModel = "facefusion"

// extensions are the file extensions a stored input image gets by its media type (the ones the generation test accepts).
var extensions = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}

// SwapRequest is "swap the face" (Issue #995): the picture to put a face into (Target: a succeeded image of the history, a Project asset, an
// upload, ...), the picture of the face (Face: one face, its largest) and the FaceFusion models (empty fields are the defaults: GHOST, YuNet
// and GFPGAN).
type SwapRequest struct {
	Target    gentest.RefInput
	Face      gentest.RefInput
	Selection facefusion.Selection
}

// SwapRef is a started swap: the batch and the job (RunRef) and the result it makes, with the license cautions of the models chosen.
type SwapRef struct {
	gentest.RunRef
	ResultID string   `json:"result_id" jsonschema:"顔を入れ替えた結果の result_id（終わるまでは status が queued / running）"`
	Warnings []string `json:"warnings,omitempty" jsonschema:"選んだモデルのライセンス上の注意（商用NG・研究用・商用不明）。空なら無い。どのモデルでも、ArcFace の注意は別（get_facefusion_status の arcface_note）"`
}

// Available says whether a swap can start: nil, or why not (the service has no FaceFusion, or it is not set up: then the common error
// facefusion_unavailable, #1010). The settings screen and
// get_facefusion_status say the same things from the lookup.
func Available(g *gentest.Service) error {
	if g.FaceFusion == nil {
		return gterr.Invalid("FaceFusion は使えません（このアプリは FaceFusion に対応していません）")
	}
	if g.FaceFusionCheck != nil {
		if err := g.FaceFusionCheck(); err != nil {
			return apperr.New(apperr.CodeFaceFusionNone).Wrap(err) // the cause and the remedy in Japanese (and the reason it was not found as the detail)
		}
	}
	return nil
}

// Swap puts the face of one picture into another with FaceFusion, as a batch of its own with one result (a file of its own: the pictures
// stay), run as a gentest job so that it is in the job list and the history like any result. FaceFusion is an external tool that the user
// installed; a swap is local, so there is nothing to price or confirm: it starts at once. The result remembers the models, the source and
// the cautions (record.FaceSwap), and its face similarity to the face picture is measured as for any result of the role "face".
// FaceFusion refuses nude and sexual pictures (its content analyser cannot be switched off): the screens say a result of an adult model is
// not one to swap, but mam does not judge the picture, so FaceFusion's own refusal is the failure the result shows.
func Swap(ctx context.Context, g *gentest.Service, r SwapRequest) (SwapRef, error) {
	if g.Jobs == nil || g.Repo == nil {
		return SwapRef{}, gterr.ErrUnavailable
	}
	if err := Available(g); err != nil {
		return SwapRef{}, err
	}
	sel := r.Selection.WithDefaults()
	warnings, err := sel.Warnings()
	if err != nil {
		return SwapRef{}, gterr.Invalid("%v", err)
	}
	target, err := g.ResolveInput(ctx, r.Target)
	if err != nil {
		return SwapRef{}, err
	}
	face, err := g.ResolveInput(ctx, r.Face)
	if err != nil {
		return SwapRef{}, err
	}
	info := record.FaceSwap{Swapper: sel.Swapper, Detector: sel.Detector, Warnings: warnings}
	if sel.Enhancer != facefusion.EnhancerNone {
		info.Enhancer = sel.Enhancer
	}
	var source gentestrepo.Result
	if r.Target.Source == gentest.FromHistory {
		if source, err = g.Repo.GetResult(ctx, r.Target.ID); err != nil {
			return SwapRef{}, gterr.NotFound(err)
		}
		info.SourceResultID = source.ID
	}
	dir := filepath.Join(g.DataDir, gentest.DirName)
	nb, err := newBatch(g, dir, source, target, face, info)
	if err != nil {
		return SwapRef{}, err
	}
	if err := g.Repo.CreateBatch(ctx, nb); err != nil {
		_ = os.RemoveAll(filepath.Join(dir, nb.Batch.ID))
		return SwapRef{}, err
	}
	job, err := g.Jobs.Enqueue(ctx, domain.JobGentest, record.Payload{BatchID: nb.Batch.ID})
	if err == nil {
		err = g.Repo.SetBatchJob(ctx, nb.Batch.ID, job.ID)
	}
	if err != nil {
		_ = g.Repo.DeleteBatch(context.WithoutCancel(ctx), nb.Batch.ID)
		_ = os.RemoveAll(filepath.Join(dir, nb.Batch.ID))
		return SwapRef{}, err
	}
	return SwapRef{RunRef: gentest.RunRef{BatchID: nb.Batch.ID, JobID: job.ID, TotalItems: 1}, ResultID: nb.Results[0].ID, Warnings: warnings}, nil
}

// newBatch copies the two pictures into the batch's directory (the target as the input of the role "edit", the face as the one of the role
// "face") and plans the one result. The batch has no estimate and no prompt of the prompt history.
func newBatch(g *gentest.Service, root string, source gentestrepo.Result, target, face gentest.Source, info record.FaceSwap) (gentestrepo.NewBatch, error) {
	now := g.Now().UTC()
	id := domain.NewID()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return gentestrepo.NewBatch{}, err
	}
	fail := func(err error) (gentestrepo.NewBatch, error) {
		_ = os.RemoveAll(dir)
		return gentestrepo.NewBatch{}, err
	}
	var inherited record.ResultParams
	_ = json.Unmarshal(source.Params, &inherited)
	aspect := inherited.AspectRatio
	if aspect == "" {
		aspect = "1:1"
	}
	params, _ := json.Marshal(record.BatchParams{AspectRatio: aspect, Count: 1})
	var inputs []gentestrepo.Input
	var used []string
	for i, p := range []struct {
		src  gentest.Source
		role string
	}{{target, gentestrepo.RoleEdit}, {face, gentestrepo.RoleFace}} {
		inputID := domain.NewID()
		name := "in-" + inputID + extensions[p.src.Mime]
		if err := fsutil.CopyFile(p.src.Path, filepath.Join(dir, name)); err != nil {
			return fail(fmt.Errorf("copy input image: %w", err))
		}
		inputs = append(inputs, gentestrepo.Input{
			ID: inputID, BatchID: id, Role: p.role, FilePath: path.Join(id, name), MimeType: p.src.Mime, SizeBytes: p.src.Size, Origin: p.src.Origin, Position: i,
		})
		used = append(used, inputID)
	}
	zero := 0.0
	result := gentestrepo.Result{
		ID: domain.NewID(), BatchID: id, Media: gentestrepo.MediaImage, Model: SwapModel, APIModel: info.Swapper,
		NegativeMode: gentestrepo.NegativeNone, UsedInputs: used, CostBasis: modelcatalog.BasisFree, EstimatedCost: &zero, CreatedAt: now,
	}
	result.Params, _ = json.Marshal(record.ResultParams{AspectRatio: aspect, FaceSwap: &info})
	return gentestrepo.NewBatch{
		Batch:   gentestrepo.Batch{ID: id, Media: gentestrepo.MediaImage, Params: params, CreatedAt: now},
		Inputs:  inputs,
		Results: []gentestrepo.Result{result},
	}, nil
}
