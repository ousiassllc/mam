// Package comfyopen is "今の設定を ComfyUI で開く" of the ComfyUI tier of the generation test (Issue #977): the model and the values of the form (the prompt,
// the size, the seed, the steps and the other sampling values, the LoRA, the values of the exposed inputs) are put into the model's workflow by the same code a
// run uses (comfyui.GraphFor), the API-format graph that makes is turned into the editor's format (workflows/graphui) and put into the ComfyUI server's
// workflows, where the node screen opens it. The node screen has no other way to take a graph (it reads a file of the server's workflows), so this is the
// way of "ワークフローとして保存して開く" without saving a workflow of mam: the file is replaced the next time.
package comfyopen

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/comfyui"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/exposedreq"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/modelcatalog"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/refs"
	"github.com/ousiassllc/mam/backend/internal/service/workflows"
	"github.com/ousiassllc/mam/backend/internal/service/workflows/graphui"
)

// Opener puts graphs into the ComfyUI of a model.
type Opener struct {
	Gentest   *gentest.Service
	Workflows *workflows.Service
}

// Opened is where the graph was put: File is the file of the server's workflows to look for in the node screen (the screen of Endpoint opens it).
type Opened struct {
	File, Endpoint, ModelLabel string
	// Notes say what of the form is not in the graph (the input images, a prompt that is translated when it is run).
	Notes []string
}

// Graph is the API-format graph the request would run for its one model: the workflow with the request's values in it. A request without a seed gets a
// new one (the graph is one run). The model must be a ComfyUI workflow.
func (o Opener) Graph(ctx context.Context, req gentest.Request) (graph json.RawMessage, endpoint, label string, notes []string, err error) {
	if req.Media != "" && req.Media != gentest.MediaImage || len(req.Models) != 1 {
		return nil, "", "", nil, gterr.Invalid("ComfyUI で開けるのは、画像のモデル 1 つの設定です。モデルを 1 つだけ選んでください")
	}
	m, err := o.Gentest.Model(ctx, gentest.MediaImage, req.Models[0])
	if err != nil {
		return nil, "", "", nil, gterr.Invalid("モデル %q は選べません", req.Models[0])
	}
	lm, _, ok := o.Gentest.Adapter.Config().LocalModel(domain.ModelName(m.ID))
	if !ok || lm.Workflow == nil {
		return nil, "", "", nil, gterr.Invalid("「%s」は ComfyUI のワークフローではないため、ノード画面では開けません", m.Label)
	}
	if err := req.Sampling.Validate(); err != nil {
		return nil, "", "", nil, gterr.Invalid("詳細設定が正しくありません: %v", err)
	}
	values, err := exposedreq.Check([]modelcatalog.Info{m}, req.Exposed)
	if err != nil {
		return nil, "", "", nil, err
	}
	img := modeladapter.ImageRequest{AspectRatio: req.AspectRatio, Seed: req.Seed}
	if len(req.Prompts) > 0 {
		img.Prompt, img.NegativePrompt = req.Prompts[0].Prompt, req.Prompts[0].Negative
	}
	if img.Seed == nil {
		seed := o.Gentest.Seed()
		img.Seed = &seed
	}
	if s := m.Sampling.Effective(m.Sampling.WithQuality(req.Quality, req.Sampling)); s != nil {
		img.Options.Sampling = *s
	}
	img.Options.Exposed, _ = exposedreq.Plan(m, values)
	if req.LoRA != nil && m.SupportsLoRA {
		l, err := refs.CheckLoRA(gentest.MediaImage, req.LoRA.Name, req.LoRA.Strength)
		if err != nil {
			return nil, "", "", nil, err
		}
		sent := (&refs.LoRA{Name: l.Name, Strength: l.Strength}).Sent()
		img.Options.LoRA = &sent
	}
	if graph, err = comfyui.GraphFor(*lm.Workflow, img); err != nil {
		return nil, "", "", nil, gterr.Invalid("グラフを作れませんでした: %v", err)
	}
	endpoint, _ = localcfg.EndpointOf(m.ID)
	notes = append(notes, "入力画像はグラフに入れていません（読み込みのノードのファイルはワークフローのままです）", "英訳をオンにしていても、プロンプトは入力したままです")
	return graph, endpoint, m.Label, notes, nil
}

// Open puts the graph of the request into the ComfyUI server's workflows.
func (o Opener) Open(ctx context.Context, req gentest.Request) (Opened, error) {
	graph, endpoint, label, notes, err := o.Graph(ctx, req)
	if err != nil {
		return Opened{}, err
	}
	info, err := o.Workflows.ObjectInfo(ctx, endpoint, true)
	if err != nil {
		return Opened{}, gterr.Invalid("%s", message(err))
	}
	ui, err := graphui.Convert(graph, info)
	if err != nil {
		return Opened{}, gterr.Invalid("ノード画面で開ける形にできませんでした: %v", err)
	}
	file, err := o.Workflows.OpenUI(ctx, endpoint, "今の設定 "+strings.TrimSpace(label), string(ui))
	if err != nil {
		return Opened{}, gterr.Invalid("%s", message(err))
	}
	return Opened{File: file, Endpoint: endpoint, ModelLabel: label, Notes: notes}, nil
}

// message is the refusal of the workflows service in words, or the error's own.
func message(err error) string {
	var in *workflows.InvalidError
	if errors.As(err, &in) {
		return in.Message
	}
	return err.Error()
}
