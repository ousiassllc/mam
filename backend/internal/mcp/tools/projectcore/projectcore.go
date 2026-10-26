// Package projectcore holds the MCP tools of a Project itself: creating one from a first message (with
// attachments and a Brand Template), reading it, its Brief and its Confirmation Level. The list of the
// Projects is tools/projects; the chat thread is tools/chat.
package projectcore

import (
	"context"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/tools/stage"
	"github.com/ousiassllc/mam/backend/internal/service"
)

// PurposeNewProjectAttachment is the upload purpose of a file the first message of a new Project carries.
const PurposeNewProjectAttachment = "new_project_attachment"

// Register adds the tools and the upload purpose of this package.
func Register(r *mcpkit.Registrar, svc *service.Services) {
	files := stage.New()
	registerPurpose(r.Files, files)

	mcpkit.Read(r, "get_project",
		"プロジェクト 1 件の状態・モード・確認レベル・Production Template・Quality Policy / One Project's state, mode, Confirmation Level, Production Template and Quality Policy.",
		getProject(svc.Projects))
	mcpkit.Write(r, "create_project",
		"最初の発言からプロジェクトを作る。Brief が発言から抽出され、AI が次の質問（または完了の知らせ）をチャットに投稿する（list_chat_messages で読む）。画像・動画は create_upload_ticket（purpose=new_project_attachment）で上げて attachment_upload_ids で渡す / Create a Project from the first message.",
		createProject(svc.Projects, files))
	mcpkit.Read(r, "list_new_project_templates",
		"新規プロジェクトに適用できる Brand Template の一覧（create_project の brand_template_id に使う）/ Brand Templates a new Project can start from.",
		listTemplates(svc.Projects))
	mcpkit.Write(r, "set_confirmation_level",
		"確認レベル（full_auto / key_check / custom_check）を変える。custom_check では確認ポイント（script / rough_cut / final）も選べる。変更はチャットにも記録される / Set the Confirmation Level.",
		setConfirmationLevel(svc.Projects), mcpkit.Idempotent())
	registerBrief(r, svc)
}

// Project is a Project with the settings of its pipeline.
type Project struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name" jsonschema:"表示名（Brief の Subject か最初の発言から）"`
	State              string    `json:"state" jsonschema:"Core State Machine の状態（DRAFT, PLANNING, AWAITING_APPROVAL, GENERATING ...）"`
	Mode               string    `json:"mode" jsonschema:"auto or director"`
	ConfirmationLevel  string    `json:"confirmation_level" jsonschema:"full_auto, key_check or custom_check"`
	CustomCheckpoints  []string  `json:"custom_checkpoints" jsonschema:"custom_check のときに確認する点（script, rough_cut, final）"`
	ProductionTemplate string    `json:"production_template" jsonschema:"product_cm, drama, anime or sns_short"`
	QualityPolicy      string    `json:"quality_policy" jsonschema:"fast, balanced or max_quality"`
	SeriesID           string    `json:"series_id,omitempty"`
	EpisodeNumber      int       `json:"episode_number,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

func projectOf(p domain.Project) Project {
	checkpoints := make([]string, 0, len(p.CustomCheckpoints))
	for _, c := range p.CustomCheckpoints {
		checkpoints = append(checkpoints, string(c))
	}
	return Project{
		ID: p.ID, Name: p.Name, State: string(p.State), Mode: string(p.Mode),
		ConfirmationLevel: string(p.ConfirmationLevel), CustomCheckpoints: checkpoints,
		ProductionTemplate: string(p.ProductionTemplate), QualityPolicy: string(p.QualityPolicy),
		SeriesID: p.SeriesID, EpisodeNumber: p.EpisodeNumber, CreatedAt: p.CreatedAt,
	}
}

// ProjectInput names a Project.
type ProjectInput struct {
	ProjectID string `json:"project_id"`
}

// ProjectOutput is one Project.
type ProjectOutput struct {
	Project Project `json:"project"`
}

func getProject(svc *service.ProjectService) mcpkit.Handler[ProjectInput, ProjectOutput] {
	return func(ctx context.Context, in ProjectInput) (ProjectOutput, error) {
		p, err := svc.Get(ctx, in.ProjectID)
		if err != nil {
			return ProjectOutput{}, err
		}
		return ProjectOutput{Project: projectOf(p)}, nil
	}
}

// CreateInput is the first message of a new Project.
type CreateInput struct {
	Content             string   `json:"content,omitempty" jsonschema:"最初の発言（作りたい動画。4000 文字まで）。添付だけのときは省略できる"`
	BrandTemplateID     string   `json:"brand_template_id,omitempty" jsonschema:"その Brand Template の制約を新しいプロジェクトにコピーする（list_new_project_templates）"`
	AttachmentUploadIDs []string `json:"attachment_upload_ids,omitempty" jsonschema:"new_project_attachment で上げたファイルの upload_id（8 件まで）。画像は Brief の Assets、動画は References に入る"`
}

func createProject(svc *service.ProjectService, files *stage.Store) mcpkit.Handler[CreateInput, ProjectOutput] {
	return func(ctx context.Context, in CreateInput) (ProjectOutput, error) {
		staged, err := files.Claim("", in.AttachmentUploadIDs)
		if err != nil {
			return ProjectOutput{}, err
		}
		p, err := svc.Create(ctx, service.CreateProjectInput{Content: in.Content, Uploads: stage.Uploads(staged), BrandTemplateID: in.BrandTemplateID})
		if err != nil {
			files.Release(staged)
			return ProjectOutput{}, err
		}
		files.Discard(staged)
		return ProjectOutput{Project: projectOf(p)}, nil
	}
}

// ListTemplatesInput has no field.
type ListTemplatesInput struct{}

// Template is a Brand Template.
type Template struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	ConstraintCount int       `json:"constraint_count"`
	CreatedAt       time.Time `json:"created_at"`
}

// ListTemplatesOutput is the Brand Templates, oldest first.
type ListTemplatesOutput struct {
	Templates []Template `json:"templates"`
}

func listTemplates(svc *service.ProjectService) mcpkit.Handler[ListTemplatesInput, ListTemplatesOutput] {
	return func(ctx context.Context, _ ListTemplatesInput) (ListTemplatesOutput, error) {
		list, err := svc.Templates(ctx)
		if err != nil {
			return ListTemplatesOutput{}, err
		}
		out := ListTemplatesOutput{Templates: make([]Template, 0, len(list))}
		for _, t := range list {
			out.Templates = append(out.Templates, Template{ID: t.ID, Name: t.Name, ConstraintCount: len(t.Constraints), CreatedAt: t.CreatedAt})
		}
		return out, nil
	}
}

// ConfirmationInput is the new Confirmation Level.
type ConfirmationInput struct {
	ProjectID   string   `json:"project_id"`
	Level       string   `json:"level" jsonschema:"full_auto, key_check or custom_check"`
	Checkpoints []string `json:"checkpoints,omitempty" jsonschema:"custom_check の確認ポイント（script, rough_cut, final）。省略すると前回の選択（なければ script と final）"`
}

func setConfirmationLevel(svc *service.ProjectService) mcpkit.Handler[ConfirmationInput, ProjectOutput] {
	return func(ctx context.Context, in ConfirmationInput) (ProjectOutput, error) {
		checkpoints := make([]domain.Checkpoint, 0, len(in.Checkpoints))
		for _, c := range in.Checkpoints {
			checkpoints = append(checkpoints, domain.Checkpoint(c))
		}
		p, err := svc.SetConfirmationLevel(ctx, in.ProjectID, domain.ConfirmationLevel(in.Level), checkpoints)
		if err != nil {
			return ProjectOutput{}, err
		}
		return ProjectOutput{Project: projectOf(p)}, nil
	}
}
