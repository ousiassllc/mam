package projectcore

import (
	"context"
	"slices"
	"strings"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func registerBrief(r *mcpkit.Registrar, svc *service.Services) {
	mcpkit.Read(r, "get_brief",
		"Creative Brief（Content Type・Objective・Subject・Target・Duration・Aspect Ratio・Mood と添付・参照・追加指示）と、まだ決まっていない項目・各項目の出どころ（user / inferred_from_request / recommended。recommended は目的から決めた仮の値）/ The Creative Brief, what is still missing and where each value came from.",
		getBrief(svc.Projects))
	mcpkit.Write(r, "update_brief",
		"Creative Brief を更新する。渡した項目だけ変わり、空文字は「未決定」に戻す（AI がまた質問する）。値は画面の編集フォームと同じ検証（Aspect Ratio は 9:16・16:9・1:1 など）。渡した項目は user の値になり、渡さなかった項目の出どころ（recommended など）は変わらない。confirm=true で、いま Brief にある推奨・読み取りの値をすべて確定する。保存するとチャットに記録され、パイプラインが再開する / Update the Brief; omitted fields keep their value and source, an empty string makes a field undecided, confirm=true accepts every recommended or inferred value.",
		updateBrief(svc.Projects), mcpkit.Idempotent())
}

// BriefAsset is a file the Brief refers to (download it with get_asset_download_url).
type BriefAsset struct {
	AssetID  string `json:"asset_id"`
	Type     string `json:"type" jsonschema:"image or video"`
	MimeType string `json:"mime_type"`
}

// BriefOutput is the Creative Brief of a Project.
type BriefOutput struct {
	ProjectID             string            `json:"project_id"`
	State                 string            `json:"state" jsonschema:"プロジェクトの状態"`
	Fields                map[string]string `json:"fields" jsonschema:"content_type, objective, subject, target, duration, aspect_ratio, mood（未決定は空文字）"`
	Missing               []string          `json:"missing" jsonschema:"まだ決まっておらず、AI に任せてもいない項目"`
	Delegated             []string          `json:"delegated" jsonschema:"AI に任せた項目"`
	Sources               map[string]string `json:"sources" jsonschema:"項目ごとの出どころ: user（利用者が決めた・確定した）/ inferred_from_request（依頼文から読み取った未確認の値）/ recommended（目的から決めた仮の値。利用者が明示した値が常に優先される）。additional_instruction も含む"`
	ProposedSubject       string            `json:"proposed_subject,omitempty" jsonschema:"Subject が食い違って利用者の判断待ちのとき、もう一方の候補（Subject はそのまま）"`
	AdditionalInstruction string            `json:"additional_instruction"`
	Style                 string            `json:"style,omitempty" jsonschema:"Series の Episode が引き継ぐ Style"`
	Assets                []BriefAsset      `json:"assets" jsonschema:"添付の画像"`
	References            []string          `json:"references" jsonschema:"参照の URL と動画アセットの ID"`
	ReferenceAssets       []BriefAsset      `json:"reference_assets" jsonschema:"references のうち動画アセットの詳細"`
}

func briefOf(v service.BriefView) BriefOutput {
	b := v.Brief
	out := BriefOutput{
		ProjectID: v.Project.ID, State: string(v.Project.State), Fields: map[string]string{},
		Missing: []string{}, Delegated: nonNil(b.Delegated), AdditionalInstruction: b.AdditionalInstruction, Style: b.Style, Sources: map[string]string{}, ProposedSubject: b.ProposedSubject,
		Assets: []BriefAsset{}, References: nonNil(b.References), ReferenceAssets: []BriefAsset{},
	}
	for _, f := range domain.RequiredBriefFields() {
		out.Fields[string(f)] = b.Get(f)
		if b.Get(f) != "" {
			out.Sources[string(f)] = b.SourceOf(f)
		}
	}
	if b.AdditionalInstruction != "" {
		out.Sources[string(domain.BriefAdditionalInstruction)] = b.SourceOf(domain.BriefAdditionalInstruction)
	}
	for _, f := range b.Missing() {
		out.Missing = append(out.Missing, string(f))
	}
	for _, a := range v.Assets {
		asset := BriefAsset{AssetID: a.ID, Type: string(a.Type), MimeType: a.MimeType}
		switch {
		case slices.Contains(b.Assets, a.ID):
			out.Assets = append(out.Assets, asset)
		case slices.Contains(b.References, a.ID):
			out.ReferenceAssets = append(out.ReferenceAssets, asset)
		}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func getBrief(svc *service.ProjectService) mcpkit.Handler[ProjectInput, BriefOutput] {
	return func(ctx context.Context, in ProjectInput) (BriefOutput, error) {
		v, err := svc.Brief(ctx, in.ProjectID)
		if err != nil {
			return BriefOutput{}, err
		}
		return briefOf(v), nil
	}
}

// UpdateBriefInput is the edited Brief. A field left out keeps its value.
type UpdateBriefInput struct {
	ProjectID             string    `json:"project_id"`
	ContentType           *string   `json:"content_type,omitempty"`
	Objective             *string   `json:"objective,omitempty" jsonschema:"200 文字まで"`
	Subject               *string   `json:"subject,omitempty" jsonschema:"200 文字まで"`
	Target                *string   `json:"target,omitempty" jsonschema:"200 文字まで"`
	Duration              *string   `json:"duration,omitempty"`
	AspectRatio           *string   `json:"aspect_ratio,omitempty" jsonschema:"9:16, 16:9 or 1:1"`
	Mood                  *string   `json:"mood,omitempty" jsonschema:"200 文字まで"`
	AdditionalInstruction *string   `json:"additional_instruction,omitempty" jsonschema:"4000 文字まで"`
	References            *[]string `json:"references,omitempty" jsonschema:"参照の URL（http:// か https://）の全件。動画アセットの参照はそのまま残る"`
	Confirm               bool      `json:"confirm,omitempty" jsonschema:"true で、Brief にある推奨・読み取りの値をすべて利用者の確定した値にする（チャットの「この内容でよいですか？」への承認と同じ）"`
}

func updateBrief(svc *service.ProjectService) mcpkit.Handler[UpdateBriefInput, BriefOutput] {
	return func(ctx context.Context, in UpdateBriefInput) (BriefOutput, error) {
		current, err := svc.Brief(ctx, in.ProjectID)
		if err != nil {
			return BriefOutput{}, err
		}
		form := service.BriefForm{
			Values:                map[domain.BriefField]string{},
			AdditionalInstruction: current.Brief.AdditionalInstruction,
			References:            strings.Join(slices.DeleteFunc(slices.Clone(current.Brief.References), isAssetID), "\n"),
			KeepProposals:         !in.Confirm,
		}
		for _, f := range domain.RequiredBriefFields() {
			form.Values[f] = current.Brief.Get(f)
		}
		set := map[domain.BriefField]*string{
			domain.BriefContentType: in.ContentType, domain.BriefObjective: in.Objective, domain.BriefSubject: in.Subject,
			domain.BriefTarget: in.Target, domain.BriefDuration: in.Duration, domain.BriefAspectRatio: in.AspectRatio, domain.BriefMood: in.Mood,
		}
		for _, f := range domain.RequiredBriefFields() {
			if v := set[f]; v != nil {
				form.Values[f] = *v
				form.Given = append(form.Given, f)
			}
		}
		if in.AdditionalInstruction != nil {
			form.AdditionalInstruction = *in.AdditionalInstruction
			form.Given = append(form.Given, domain.BriefAdditionalInstruction)
		}
		if in.References != nil {
			form.References = strings.Join(*in.References, "\n")
		}
		v, err := svc.UpdateBrief(ctx, in.ProjectID, form)
		if err != nil {
			return BriefOutput{}, err
		}
		return briefOf(v), nil
	}
}

// isAssetID tells a video asset ID of Brief.References from a URL (the service keeps the former when it replaces the latter).
func isAssetID(ref string) bool {
	return !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://")
}
