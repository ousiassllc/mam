// Package shotgen holds the MCP tools that start work on a Shot or a Take: generation, Prompt Candidates,
// the Reference Pack, Continue, the AI Video Judge and the Repair Agent, and the reads of their results.
// Every operation that takes time returns a job_id; get_job reads its Probe (probes.go).
package shotgen

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/jobs"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// Job kinds this package adds. Generation, Prompt Candidates and Continue use jobs.KindGenerateShot.
const (
	KindReferencePack = "build_reference_pack"
	KindJudgeTake     = "judge_take"
	KindRepairTake    = "repair_take"
)

// Register adds the tools of this package and the Probes of its job kinds.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	r.Jobs.RegisterKind(KindReferencePack, referencePackProbe(svc.Generation))
	r.Jobs.RegisterKind(KindJudgeTake, judgeProbe(svc.Generation, svc.Trace))
	r.Jobs.RegisterKind(KindRepairTake, repairProbe(svc.Generation, svc.Trace))

	mcpkit.Write(r, "generate_shot",
		"Shot の生成を始める（単発の生成）。job_id を返す。進捗・結果（take_ids）は get_job / Start generating a Shot; returns a job_id.",
		generate(r, jobs.KindGenerateShot, svc.Generation.GenerateShot), mcpkit.OpenWorld())
	mcpkit.Write(r, "generate_shot_candidates",
		"Shot の Prompt Candidate Generation（複数の撮影戦略で生成し Judge で Best Take を選ぶ）を始める。job_id を返す / Start Prompt Candidate Generation for a Shot.",
		generate(r, jobs.KindGenerateShot, svc.Generation.GenerateCandidates), mcpkit.OpenWorld())
	mcpkit.Write(r, "build_reference_pack",
		"Shot の Reference Pack を（再）生成する。job_id を返す / (Re)build the Reference Pack of a Shot.",
		buildPack(r, svc.Generation), mcpkit.OpenWorld())
	mcpkit.Write(r, "propose_continue",
		"完了した Shot の続きの展開候補を、チャットの質問カードとして提案する（同期。confirm_continue で答える）/ Propose how to continue after a completed Shot.",
		propose(svc.Continue), mcpkit.OpenWorld())
	mcpkit.Write(r, "confirm_continue",
		"続きの Shot を作って生成を始める（instruction か delegate=true）。job_id は新しい Shot の生成 / Make the next Shot and start generating it.",
		confirm(r, svc.Continue), mcpkit.OpenWorld())
	mcpkit.Write(r, "judge_take",
		"Take に AI Video Judge をかける。job_id を返し、結果は get_job か get_take_judge_result / Run the AI Video Judge on a Take.",
		judge(r, svc.Judge), mcpkit.OpenWorld())
	mcpkit.Write(r, "repair_take",
		"Take を Judge 結果から Repair（新しい Take を作る）。job_id を返す / Repair a Take from its Judge result.",
		repair(r, svc.Repair), mcpkit.OpenWorld())
	mcpkit.Read(r, "get_take_judge_result",
		"Take の最新の AI Video Judge 結果（GET /api/v1/takes/:id/judge-result）。未 Judge は not_found / Latest Judge result of a Take.",
		getJudgeResult(svc.Judge))
	mcpkit.Read(r, "get_jev_shadow_report",
		"Repair Agent の Jev シャドー実行のレポート（一致率・信頼度の分布・遅延・コスト）/ Report of the Jev shadow runs of the Repair Agent.",
		getShadowReport(svc.Repair))
}

// StartInput names the Shot to generate.
type StartInput struct {
	ShotID                string `json:"shot_id"`
	AdditionalInstruction string `json:"additional_instruction,omitempty" jsonschema:"追加の指示（例: もう少しカメラを寄せて）"`
}

// ShotJobOutput is the job of an operation on a Shot.
type ShotJobOutput struct {
	mcpkit.JobRef
	ShotID    string `json:"shot_id"`
	ShotState string `json:"shot_state"`
}

type shotAction func(ctx context.Context, shotID, instruction string) (service.ShotFragment, error)

func generate(r *mcpkit.Registrar, kind string, act shotAction) mcpkit.Handler[StartInput, ShotJobOutput] {
	return func(ctx context.Context, in StartInput) (ShotJobOutput, error) {
		var frag service.ShotFragment
		ref, err := r.Jobs.Run(ctx, kind, in.ShotID, func(ctx context.Context) (string, error) {
			var err error
			frag, err = act(ctx, in.ShotID, in.AdditionalInstruction)
			return frag.JobID, err
		})
		if err != nil {
			return ShotJobOutput{}, err
		}
		return ShotJobOutput{JobRef: ref, ShotID: frag.ID, ShotState: string(frag.State)}, nil
	}
}

// ShotInput names a Shot.
type ShotInput struct {
	ShotID string `json:"shot_id"`
}

func buildPack(r *mcpkit.Registrar, svc *service.GenerationService) mcpkit.Handler[ShotInput, ShotJobOutput] {
	return func(ctx context.Context, in ShotInput) (ShotJobOutput, error) {
		var frag service.ShotFragment
		ref, err := r.Jobs.Run(ctx, KindReferencePack, in.ShotID, func(ctx context.Context) (string, error) {
			var err error
			frag, err = svc.BuildReferencePack(ctx, in.ShotID)
			return frag.JobID, err
		})
		if err != nil {
			return ShotJobOutput{}, err
		}
		return ShotJobOutput{JobRef: ref, ShotID: frag.ID, ShotState: string(frag.State)}, nil
	}
}
