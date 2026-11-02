// Package export packs results of the generation test history into ZIP files (#932): the images and videos in photos/ and one
// prompts/prompts.csv that says, for every file, when and with which model, seed and prompt it was made.
//
// Which results go in is decided here and only here (Selection): the history screen and the MCP tool
// export_generation_results both call Exporter, so a choice means the same on both. The ZIP is streamed (Archive.Write): the
// files are copied one by one and the CSV is written row by row, so a big export never sits in memory.
package export

import (
	"context"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/domain/modellicense"
	"github.com/ousiassllc/mam/backend/internal/repository/gentestrepo"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
)

const (
	// DefaultLatest is how many of the newest results are taken when nothing says how many and no result or batch is named.
	DefaultLatest = 40
	// MaxItems is the most results one export takes (all parts together).
	MaxItems = 500
	// MaxParts is the most ZIP files one export is split into.
	MaxParts = 20
	// listPage is how many results of the history are read at a time while the selection is filled.
	listPage = 200
)

// Selection says which results to export. Every field that is set narrows the choice; only succeeded results with a file count.
type Selection struct {
	// ResultIDs names the results (the ones chosen on the screen).
	ResultIDs []string
	// BatchID takes the results of one batch.
	BatchID string
	// Latest keeps the newest N of what matches (at most MaxItems). 0 is DefaultLatest, unless ResultIDs or BatchID name what to
	// take: then it is all of them.
	Latest       int
	FavoriteOnly bool
	// Models keeps the results of any of these models; Media is "image" or "video".
	Models []string
	Media  string
	// Commercial keeps the results of models the commercial-use filter allows ("all", "ok" or "ok_or_conditional"); "" is "all".
	// It is not the setting's default (modellicense.DefaultFilter): an export that leaves results out has to be asked to.
	Commercial string
}

// Item is one result of an export. Number counts from 1 in the order of the ZIP (oldest first) and is the prefix of File.
type Item struct {
	Number    int       `json:"number"`
	File      string    `json:"file" jsonschema:"photos/ の中のファイル名（prompts.csv の file 列と同じ）"`
	ResultID  string    `json:"result_id"`
	Media     string    `json:"media"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`

	row  gentestrepo.Result
	path string
}

// Plan is what a Selection comes to: the items in ZIP order and the ZIP files they are split into.
type Plan struct {
	Items []Item
	// Skipped are the chosen result ids that are not exported: not in the history, no file, or left out by another condition.
	Skipped []string
	Parts   []Part
}

// Part is one ZIP file of a Plan.
type Part struct {
	// Ref names its results (Exporter.Open); Name is the file name the ZIP is offered under.
	Ref  Ref
	Name string
	// First and Last are the numbers of its first and last item.
	First, Last int
	Items       int
	// Bytes is the total size of the files that go in (not the size of the ZIP).
	Bytes int64
}

// Split says when a Plan starts another ZIP: after Items results, or before the result that would take the files past Bytes (one
// that is bigger alone is a ZIP of its own). 0 is no limit.
type Split struct {
	Items int
	Bytes int64
}

// Exporter reads the history of a generation test Service.
type Exporter struct{ svc *gentest.Service }

// New returns the Exporter of svc.
func New(svc *gentest.Service) *Exporter { return &Exporter{svc: svc} }

// Plan resolves the selection and splits the result into ZIP files. It refuses a selection that has nothing to export.
func (e *Exporter) Plan(ctx context.Context, sel Selection, split Split) (Plan, error) {
	if split.Items < 0 || split.Bytes < 0 {
		return Plan{}, gterr.Invalid("分割の大きさは 0 以上にしてください")
	}
	items, skipped, err := e.resolve(ctx, sel)
	if err != nil {
		return Plan{}, err
	}
	number(items, 1, len(items))
	spans := splitSpans(items, split)
	if len(spans) > MaxParts {
		return Plan{}, gterr.Invalid("%d 個の ZIP に分かれてしまいます（上限 %d）。分割の大きさを大きくするか、件数を減らしてください", len(spans), MaxParts)
	}
	p := Plan{Items: items, Skipped: skipped}
	for i, span := range spans {
		part := items[span[0]:span[1]]
		pt := Part{First: part[0].Number, Last: part[len(part)-1].Number, Items: len(part)}
		pt.Ref = Ref{Part: i + 1, Parts: len(spans), First: pt.First, Total: len(items)}
		for _, it := range part {
			pt.Ref.IDs = append(pt.Ref.IDs, it.ResultID)
			pt.Bytes += it.row.SizeBytes
		}
		pt.Name = archiveName(newest(part), len(part), pt.Ref.Part, pt.Ref.Parts)
		p.Parts = append(p.Parts, pt)
	}
	return p, nil
}

// Archive resolves the selection into one ZIP (what the history screen downloads).
func (e *Exporter) Archive(ctx context.Context, sel Selection) (Archive, error) {
	items, _, err := e.resolve(ctx, sel)
	if err != nil {
		return Archive{}, err
	}
	number(items, 1, len(items))
	return Archive{Name: archiveName(newest(items), len(items), 1, 1), Items: items}, nil
}

// Open is the ZIP of a Ref that a Plan made: the same results read again, with the numbers they had. A result deleted since is left
// out; when none is left it is an *gterr.InvalidError.
func (e *Exporter) Open(ctx context.Context, ref Ref) (Archive, error) {
	items, _, err := e.resolve(ctx, Selection{ResultIDs: ref.IDs})
	if err != nil {
		return Archive{}, err
	}
	number(items, ref.First, ref.Total)
	return Archive{Name: archiveName(newest(items), len(items), ref.Part, ref.Parts), Items: items}, nil
}

// resolve reads the results the selection takes, oldest first, with their files checked. The numbers are not set yet.
func (e *Exporter) resolve(ctx context.Context, sel Selection) ([]Item, []string, error) {
	f, allows, limit, err := e.filter(sel)
	if err != nil {
		return nil, nil, err
	}
	var items []Item
	for offset := 0; len(items) < limit; offset += listPage {
		f.Offset = offset
		page, err := e.svc.Repo.ListResults(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range page {
			if len(items) == limit || !allows(r.Model) {
				continue
			}
			if file, err := e.svc.ResultFile(ctx, r.ID); err == nil && isFile(file.Path) {
				items = append(items, Item{ResultID: r.ID, Media: r.Media, Model: r.Model, CreatedAt: r.CreatedAt, row: r, path: file.Path})
			}
		}
		if len(page) < listPage {
			break
		}
	}
	var skipped []string
	for _, id := range f.IDs {
		if !slices.ContainsFunc(items, func(it Item) bool { return it.ResultID == id }) {
			skipped = append(skipped, id)
		}
	}
	if len(items) == 0 {
		return nil, skipped, gterr.Invalid("書き出せる結果がありません（条件に合う、成功してファイルのある結果がありません）")
	}
	slices.SortStableFunc(items, func(a, b Item) int {
		switch {
		case a.CreatedAt.Before(b.CreatedAt):
			return -1
		case a.CreatedAt.After(b.CreatedAt):
			return 1
		case a.row.BatchID != b.row.BatchID:
			return strings.Compare(a.row.BatchID, b.row.BatchID)
		case a.row.Position != b.row.Position:
			return a.row.Position - b.row.Position
		}
		return strings.Compare(a.ResultID, b.ResultID)
	})
	return items, skipped, nil
}

// filter validates the selection and turns it into the repository's filter, the commercial-use check of a model and the number of
// results to take.
func (e *Exporter) filter(sel Selection) (gentestrepo.Filter, func(model string) bool, int, error) {
	var zero gentestrepo.Filter
	if sel.Media != "" && sel.Media != gentest.MediaImage && sel.Media != gentest.MediaVideo {
		return zero, nil, 0, gterr.Invalid("種類は image か video にしてください")
	}
	if sel.Latest < 0 || sel.Latest > MaxItems {
		return zero, nil, 0, gterr.Invalid("件数は 1〜%d にしてください", MaxItems)
	}
	allows := func(string) bool { return true }
	if sel.Commercial != "" {
		cf, ok := modellicense.ParseFilter(sel.Commercial)
		if !ok {
			return zero, nil, 0, gterr.Invalid("commercial は all / ok / ok_or_conditional のどれかです")
		}
		allows = func(model string) bool { return cf.Allows(modellicense.Of(model).Commercial) }
	}
	ids, err := cleanIDs(sel.ResultIDs)
	if err != nil {
		return zero, nil, 0, err
	}
	limit := sel.Latest
	if limit == 0 {
		limit = DefaultLatest
		if len(ids) > 0 || sel.BatchID != "" {
			limit = MaxItems
		}
	}
	return gentestrepo.Filter{
		IDs: ids, BatchID: sel.BatchID, Models: sel.Models, Media: sel.Media, FavoriteOnly: sel.FavoriteOnly, SucceededOnly: true, Limit: listPage,
	}, allows, limit, nil
}

var idPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// cleanIDs drops blanks and repeats and refuses an id that cannot be one (ids go into a SQL list and a download ticket).
// A choice that names ids and ends up with none is refused, not turned into "everything".
func cleanIDs(in []string) ([]string, error) {
	if len(in) > MaxItems {
		return nil, gterr.Invalid("一度に書き出せるのは %d 件までです", MaxItems)
	}
	var out []string
	for _, id := range in {
		if id = strings.TrimSpace(id); id == "" || slices.Contains(out, id) {
			continue
		}
		if !idPattern.MatchString(id) {
			return nil, gterr.Invalid("結果の id が正しくありません: %q", id)
		}
		out = append(out, id)
	}
	if len(in) > 0 && len(out) == 0 {
		return nil, gterr.Invalid("結果の id が空です")
	}
	return out, nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// number sets the numbers and file names of items: they count from first, and total says how wide a number is.
func number(items []Item, first, total int) {
	for i := range items {
		it := &items[i]
		it.Number = first + i
		it.File = fileName(it.Number, total, it.CreatedAt, it.Model, it.ResultID, it.row.FilePath, it.row.MimeType)
	}
}

func newest(items []Item) time.Time {
	var t time.Time
	for _, it := range items {
		if it.CreatedAt.After(t) {
			t = it.CreatedAt
		}
	}
	return t
}

// splitSpans cuts items into runs [from, to): a run ends after split.Items results, or before the result that would take its files
// past split.Bytes.
func splitSpans(items []Item, split Split) [][2]int {
	var spans [][2]int
	start, size := 0, int64(0)
	for i, it := range items {
		if i > start && (split.Items > 0 && i-start >= split.Items || split.Bytes > 0 && size+it.row.SizeBytes > split.Bytes) {
			spans = append(spans, [2]int{start, i})
			start, size = i, 0
		}
		size += it.row.SizeBytes
	}
	return append(spans, [2]int{start, len(items)})
}
