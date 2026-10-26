package gentest

import (
	"context"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/projectlink"
)

// ListResultsInput filters the history.
type ListResultsInput struct {
	Model    string `json:"model,omitempty" jsonschema:"モデルの id で絞る"`
	Media    string `json:"media,omitempty" jsonschema:"image / video"`
	Favorite bool   `json:"favorite,omitempty" jsonschema:"true でお気に入りだけ"`
	Limit    int    `json:"limit,omitempty" jsonschema:"1〜200、既定 50"`
	Offset   int    `json:"offset,omitempty"`
}

// ListResultsOutput is the history.
type ListResultsOutput struct {
	Results []gentest.ResultView `json:"results"`
	Spend   gentest.Spend        `json:"spend" jsonschema:"履歴全体の合計コスト（絞り込みに関係なく。解析画面には出ない）"`
}

func (h handlers) listResults(ctx context.Context, in ListResultsInput) (ListResultsOutput, error) {
	g, err := h.gt()
	if err != nil {
		return ListResultsOutput{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	list, err := g.History(ctx, gentest.HistoryFilter{Model: in.Model, Media: in.Media, FavoriteOnly: in.Favorite, Limit: min(limit, 200), Offset: max(in.Offset, 0)})
	if err != nil {
		return ListResultsOutput{}, mapError(err)
	}
	spend, err := g.Spend(ctx)
	return ListResultsOutput{Results: list, Spend: spend}, mapError(err)
}

// ResultInput names a result.
type ResultInput struct {
	ResultID string `json:"result_id" jsonschema:"get_generation_test / list_generation_results の result_id"`
}

// ResultOutput is a result with the URL of its file.
type ResultOutput struct {
	gentest.ResultDetail
	File *mcpkit.Link `json:"file,omitempty" jsonschema:"成功した結果のファイルのダウンロード URL（15 分有効）"`
}

func (h handlers) getResult(ctx context.Context, in ResultInput) (ResultOutput, error) {
	g, err := h.gt()
	if err != nil {
		return ResultOutput{}, err
	}
	d, err := g.Result(ctx, in.ResultID)
	if err != nil {
		return ResultOutput{}, mapError(err)
	}
	out := ResultOutput{ResultDetail: d}
	if d.HasFile {
		link, err := h.registrar.Files.DownloadURL(ctx, KindGentestFile, in.ResultID)
		if err != nil {
			return ResultOutput{}, mapError(err)
		}
		out.File = &link
	}
	return out, nil
}

// DeletedOutput says what was deleted.
type DeletedOutput struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

func (h handlers) deleteResult(ctx context.Context, in ResultInput) (DeletedOutput, error) {
	g, err := h.gt()
	if err != nil {
		return DeletedOutput{}, err
	}
	if err := g.DeleteResult(ctx, in.ResultID); err != nil {
		return DeletedOutput{}, mapError(err)
	}
	return DeletedOutput{ID: in.ResultID, Deleted: true}, nil
}

// FavoriteInput marks a result.
type FavoriteInput struct {
	ResultID string `json:"result_id"`
	Favorite bool   `json:"favorite" jsonschema:"true で付ける、false で外す"`
}

// FavoriteOutput is the mark after the change.
type FavoriteOutput struct {
	ResultID string `json:"result_id"`
	Favorite bool   `json:"favorite"`
}

func (h handlers) setFavorite(ctx context.Context, in FavoriteInput) (FavoriteOutput, error) {
	g, err := h.gt()
	if err != nil {
		return FavoriteOutput{}, err
	}
	if err := g.SetFavorite(ctx, in.ResultID, in.Favorite); err != nil {
		return FavoriteOutput{}, mapError(err)
	}
	return FavoriteOutput(in), nil
}

// ListPromptsOutput is the prompt history.
type ListPromptsOutput struct {
	Prompts []gentest.PromptView `json:"prompts"`
}

func (h handlers) listPrompts(ctx context.Context, _ Input) (ListPromptsOutput, error) {
	g, err := h.gt()
	if err != nil {
		return ListPromptsOutput{}, err
	}
	list, err := g.Prompts(ctx)
	return ListPromptsOutput{Prompts: list}, mapError(err)
}

// promptRef names a prompt of the history.
type promptRef struct {
	PromptID string `json:"prompt_id" jsonschema:"list_generation_prompts の prompt_id"`
}

func (h handlers) deletePrompt(ctx context.Context, in promptRef) (DeletedOutput, error) {
	g, err := h.gt()
	if err != nil {
		return DeletedOutput{}, err
	}
	if err := g.DeletePrompt(ctx, in.PromptID); err != nil {
		return DeletedOutput{}, mapError(err)
	}
	return DeletedOutput{ID: in.PromptID, Deleted: true}, nil
}

// ImportInput is projectlink.ImportRequest.
type ImportInput = projectlink.ImportRequest

func (h handlers) importResult(ctx context.Context, in ImportInput) (projectlink.ImportOutcome, error) {
	g, err := h.gt()
	if err != nil {
		return projectlink.ImportOutcome{}, err
	}
	out, err := g.Import(ctx, in)
	return out, mapError(err)
}
