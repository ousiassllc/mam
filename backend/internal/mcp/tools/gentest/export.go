package gentest

import (
	"context"
	"errors"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/export"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

// KindGentestExport is the download kind of a ZIP of results; the resource id is an export.Ref (the results that were chosen when
// the URL was issued), which only the server can make: the URL of a ticket carries nothing but a random value.
const KindGentestExport = "gentest_export"

// mebibyte is the unit of split_mb.
const mebibyte = 1 << 20

func registerExport(r *mcpkit.Registrar, h handlers) {
	r.Files.RegisterDownload(KindGentestExport, h.exportFile)
	mcpkit.Read(r, "export_generation_results",
		"生成テストの結果を ZIP にまとめるダウンロード URL を発行する（15 分有効）。ZIP は photos/（画像・動画。01_日時_モデル_idの末尾.png のように番号つき）と prompts/prompts.csv（file, created_at（JST）, media, model, seed, prompt, sent_prompt, result_id。UTF-8 BOM つき。file は photos/ のファイル名と同じ）。選び方: result_ids（指定）/ batch_id / latest（直近 N 件、最大 500。何も指定しなければ直近 40 件）に、favorite・models・media・commercial（all / ok / ok_or_conditional。省くと all）が重なる。大きいときは split_items（1 つの ZIP の最大件数）・split_mb（ファイル合計の目安 MB）で複数の ZIP に分けられ、番号は全体で通し番号 / Download URL(s) of a ZIP of generation results: photos/ and prompts/prompts.csv.",
		h.exportResults)
}

// ExportInput says which results go into the ZIP and whether to split it (export.Selection and export.Split).
type ExportInput struct {
	ResultIDs  []string `json:"result_ids,omitempty" jsonschema:"書き出す結果の result_id（最大 500）"`
	BatchID    string   `json:"batch_id,omitempty" jsonschema:"このバッチの結果を書き出す"`
	Latest     int      `json:"latest,omitempty" jsonschema:"新しい順に N 件（1〜500）。result_ids / batch_id を指定したときは省くと全部、どちらも無いときは 40"`
	Favorite   bool     `json:"favorite,omitempty" jsonschema:"true でお気に入りだけ"`
	Models     []string `json:"models,omitempty" jsonschema:"これらのモデル id のどれかで作った結果だけ"`
	Media      string   `json:"media,omitempty" jsonschema:"image / video"`
	Commercial string   `json:"commercial,omitempty" jsonschema:"モデルの商用利用で絞り込む。all（すべて）/ ok（商用OKだけ）/ ok_or_conditional（商用OKと条件つき）。省くと all（設定「商用OKのモデルだけを使う」には従わない）"`
	SplitItems int      `json:"split_items,omitempty" jsonschema:"1 つの ZIP に入れる最大件数。省くと分けない"`
	SplitMB    int      `json:"split_mb,omitempty" jsonschema:"1 つの ZIP に入れるファイルの合計の上限（MB）。超える結果から次の ZIP にする。1 件がそれより大きいときはその 1 件で 1 つの ZIP。省くと分けない"`
}

// ExportFile is one ZIP of the export.
type ExportFile struct {
	mcpkit.Link
	Part  int   `json:"part" jsonschema:"何番目の ZIP か（1 から）"`
	Items int   `json:"items" jsonschema:"この ZIP の件数"`
	First int   `json:"first" jsonschema:"この ZIP の最初の番号"`
	Last  int   `json:"last" jsonschema:"この ZIP の最後の番号"`
	Bytes int64 `json:"bytes" jsonschema:"入るファイルの合計バイト数（ZIP 自体の大きさではない）"`
}

// ExportOutput lists the ZIP files and what is in them.
type ExportOutput struct {
	Files   []ExportFile  `json:"files"`
	Items   []export.Item `json:"items" jsonschema:"入る結果（number と file は prompts.csv の行と対応する）"`
	Skipped []string      `json:"skipped,omitempty" jsonschema:"指定したが書き出せなかった result_id（無い・ファイルが無い・ほかの条件に合わない）"`
}

func (h handlers) exportResults(ctx context.Context, in ExportInput) (ExportOutput, error) {
	g, err := h.gt()
	if err != nil {
		return ExportOutput{}, err
	}
	plan, err := export.New(g).Plan(ctx, export.Selection{
		ResultIDs: in.ResultIDs, BatchID: in.BatchID, Latest: in.Latest, FavoriteOnly: in.Favorite,
		Models: in.Models, Media: in.Media, Commercial: in.Commercial,
	}, export.Split{Items: in.SplitItems, Bytes: int64(max(in.SplitMB, 0)) * mebibyte})
	if err != nil {
		return ExportOutput{}, mapError(err)
	}
	out := ExportOutput{Items: plan.Items, Skipped: plan.Skipped}
	for _, p := range plan.Parts {
		link, err := h.registrar.Files.DownloadURL(ctx, KindGentestExport, p.Ref.String())
		if err != nil {
			return ExportOutput{}, mapError(err)
		}
		out.Files = append(out.Files, ExportFile{Link: link, Part: p.Ref.Part, Items: p.Items, First: p.First, Last: p.Last, Bytes: p.Bytes})
	}
	return out, nil
}

// exportFile is the download kind: it reads the results of its Ref again (so a result deleted since is left out) and streams the ZIP.
func (h handlers) exportFile(ctx context.Context, id string) (mcpkit.DownloadFile, error) {
	g, err := h.gt()
	if err != nil {
		return mcpkit.DownloadFile{}, err
	}
	ref, err := export.ParseRef(id)
	if err != nil {
		return mcpkit.DownloadFile{}, mapError(gterr.ErrNotFound)
	}
	arc, err := export.New(g).Open(ctx, ref)
	var invalid *gterr.InvalidError
	if errors.As(err, &invalid) {
		return mcpkit.DownloadFile{}, mapError(gterr.ErrNotFound) // every result of it has been deleted
	}
	if err != nil {
		return mcpkit.DownloadFile{}, mapError(err)
	}
	return mcpkit.DownloadFile{Name: arc.Name, ContentType: export.ContentType, Write: arc.Write}, nil
}
