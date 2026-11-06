package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/export"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gterr"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/history"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/record"
)

func prompt(s string) gentest.PromptInput { return gentest.PromptInput{Prompt: s} }

// images runs a batch of one image model and one prompt per text and returns its results (they made files).
func images(t *testing.T, r *gentesttest.Rig, model string, prompts ...string) []record.ResultView {
	t.Helper()
	req := gentest.Request{Media: "image", Models: []string{model}}
	for _, p := range prompts {
		req.Prompts = append(req.Prompts, prompt(p))
	}
	ref, err := r.Confirmed(req)
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, len(prompts), st.Succeeded)
	return st.Results
}

func video(t *testing.T, r *gentesttest.Rig, model, text string) record.ResultView {
	t.Helper()
	ref, err := r.Confirmed(gentest.Request{Media: "video", Models: []string{model}, Prompts: []gentest.PromptInput{prompt(text)}, DurationSeconds: 5})
	require.NoError(t, err)
	st := r.Wait(ref.BatchID)
	require.Equal(t, 1, st.Succeeded, st.Results)
	return st.Results[0]
}

// unzip writes the archive and returns its entries by name, in ZIP order.
func unzip(t *testing.T, a export.Archive) (names []string, files map[string][]byte) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, a.Write(&buf))
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err, "the body is a ZIP")
	files = map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		names = append(names, f.Name)
		files[f.Name] = data
	}
	return names, files
}

// sheet parses prompts/prompts.csv the way a spreadsheet does (after its BOM) and returns the rows without the header.
func sheet(t *testing.T, files map[string][]byte) [][]string {
	t.Helper()
	raw, ok := files[export.PromptsFile]
	require.True(t, ok, "prompts/prompts.csv is in the ZIP")
	require.True(t, bytes.HasPrefix(raw, []byte("\xEF\xBB\xBF")), "UTF-8 BOM so that Excel does not garble it")
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF")))).ReadAll()
	require.NoError(t, err, "the CSV parses back")
	require.NotEmpty(t, rows)
	assert.Equal(t, []string{"file", "created_at", "media", "model", "seed", "prompt", "sent_prompt", "result_id"}, rows[0])
	return rows[1:]
}

func TestArchiveHasPhotosAndPromptsFoldersAndTheSheetMatchesTheFiles(t *testing.T) {
	r := gentesttest.New(t)
	hard := "a fox, \"in the snow\"\nsecond line, with a comma"
	res := images(t, r, "flux-2", "a red fox", hard, "日本語のプロンプト")
	ex := export.New(r.Svc)

	arc, err := ex.Archive(context.Background(), export.Selection{BatchID: res[0].BatchID})
	require.NoError(t, err)
	names, files := unzip(t, arc)

	for _, n := range names {
		assert.True(t, n == export.PromptsFile || strings.HasPrefix(n, export.PhotosDir), "only photos/ and prompts/ exist: %s", n)
	}
	rows := sheet(t, files)
	require.Len(t, rows, 3)
	pattern := regexp.MustCompile(`^0[1-3]_\d{8}-\d{6}_flux-2_[0-9a-f]{6}\.png$`)
	byID := map[string]record.ResultView{}
	for _, v := range res {
		byID[v.ID] = v
	}
	for i, row := range rows {
		require.Len(t, row, 8)
		assert.Regexp(t, pattern, row[0])
		assert.True(t, strings.HasPrefix(row[0], "0"+string(rune('1'+i))+"_"), "the number of the file name follows the row: %s", row[0])
		assert.Equal(t, gentesttest.PNG, files[export.PhotosDir+row[0]], "the file of the row is in photos/ with the bytes of the result")
		v := byID[row[7]]
		require.NotEmpty(t, v.ID, "result_id names a result")
		assert.Equal(t, v.Prompt, row[5])
		assert.Equal(t, v.SentPrompt, row[6])
		assert.Equal(t, "image", row[2])
		assert.Equal(t, "flux-2", row[3])
		require.NotNil(t, v.Seed)
		assert.Equal(t, strconv.FormatInt(*v.Seed, 10), row[4], "the seed that was sent")
		assert.Equal(t, v.CreatedAt.In(jst()).Format("2006-01-02 15:04:05"), row[1], "created_at is JST")
	}
	assert.Len(t, names, 4, "the sheet and three files, nothing else")
	assert.Equal(t, hard, rows[1][5], "newline, comma and quotes in a prompt survive")
	assert.Equal(t, "日本語のプロンプト", rows[2][5])
	assert.Regexp(t, `^generation-results_\d{8}-\d{6}_3\.zip$`, arc.Name)
}

func TestACellThatStartsAFormulaIsMarkedAsText(t *testing.T) {
	r := gentesttest.New(t)
	res := images(t, r, "flux-2", `=HYPERLINK("http://evil.example","x")`, "@SUM(1+1)", "-2+3", "+cmd|' /C calc'!A0", "a fox = a dog")
	arc, err := export.New(r.Svc).Archive(context.Background(), export.Selection{BatchID: res[0].BatchID})
	require.NoError(t, err)
	_, files := unzip(t, arc)
	got := map[string]string{}
	for _, row := range sheet(t, files) {
		got[row[7]] = row[5]
	}
	want := map[string]string{
		res[0].ID: `'=HYPERLINK("http://evil.example","x")`,
		res[1].ID: "'@SUM(1+1)",
		res[2].ID: "'-2+3",
		res[3].ID: "'+cmd|' /C calc'!A0",
		res[4].ID: "a fox = a dog",
	}
	assert.Equal(t, want, got, "only a cell that starts with = + - @ is marked; the rest is as it was")
}

func TestSelectionTakesTheNewestNOrABatchOrTheChosenOnes(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	ex := export.New(r.Svc)
	first := images(t, r, "flux-2", "one", "two", "three")
	second := images(t, r, "nano-banana", "four", "five")

	latest, err := ex.Plan(ctx, export.Selection{Latest: 3}, export.Split{})
	require.NoError(t, err)
	hist, err := r.Svc.History(ctx, history.Filter{Limit: 3})
	require.NoError(t, err)
	assert.ElementsMatch(t, ids(hist), idsOf(latest.Items), "the newest three are the first three of the history list")
	assert.Equal(t, []int{1, 2, 3}, []int{latest.Items[0].Number, latest.Items[1].Number, latest.Items[2].Number})
	assert.Equal(t, second[1].ID, latest.Items[2].ResultID, "oldest first, so the newest is the last number")

	all, err := ex.Plan(ctx, export.Selection{}, export.Split{})
	require.NoError(t, err)
	assert.Len(t, all.Items, 5, "no condition at all is the newest 40: here, everything")

	batch, err := ex.Plan(ctx, export.Selection{BatchID: first[0].BatchID}, export.Split{})
	require.NoError(t, err)
	assert.ElementsMatch(t, ids(first), idsOf(batch.Items))

	chosen, err := ex.Plan(ctx, export.Selection{ResultIDs: []string{second[1].ID, first[0].ID, first[0].ID, "  "}}, export.Split{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{second[1].ID, first[0].ID}, idsOf(chosen.Items), "a repeated id counts once")

	withUnknown, err := ex.Plan(ctx, export.Selection{ResultIDs: []string{first[0].ID, "does-not-exist"}}, export.Split{})
	require.NoError(t, err)
	assert.Equal(t, []string{"does-not-exist"}, withUnknown.Skipped, "what cannot be exported is reported, not silently dropped")
	assert.Len(t, withUnknown.Items, 1)

	narrow, err := ex.Plan(ctx, export.Selection{ResultIDs: ids(first), Models: []string{"nano-banana"}}, export.Split{})
	assert.Error(t, err, "the chosen ones that the other conditions leave out are nothing")
	assert.Empty(t, narrow.Items)
}

func TestSelectionNarrowsByFavoriteModelMediaAndCommercialUse(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	ex := export.New(r.Svc)
	flux := images(t, r, "flux-2", "fox", "cat")
	banana := images(t, r, "nano-banana", "dog")
	clip := video(t, r, "luma-ray-3.2", "waves")
	require.NoError(t, r.Svc.SetFavorite(ctx, flux[1].ID, true))
	require.NoError(t, r.Svc.SetFavorite(ctx, clip.ID, true))

	pick := func(sel export.Selection) []string {
		t.Helper()
		p, err := ex.Plan(ctx, sel, export.Split{})
		require.NoError(t, err)
		return idsOf(p.Items)
	}
	assert.ElementsMatch(t, []string{flux[1].ID, clip.ID}, pick(export.Selection{FavoriteOnly: true}))
	assert.ElementsMatch(t, ids(flux), pick(export.Selection{Models: []string{"flux-2"}}))
	assert.ElementsMatch(t, []string{flux[0].ID, flux[1].ID, banana[0].ID}, pick(export.Selection{Media: "image"}))
	assert.Equal(t, []string{clip.ID}, pick(export.Selection{Media: "video"}))
	assert.ElementsMatch(t, []string{flux[1].ID}, pick(export.Selection{FavoriteOnly: true, Media: "image"}), "conditions add up")
	assert.ElementsMatch(t, []string{flux[0].ID, flux[1].ID, banana[0].ID}, pick(export.Selection{Commercial: "ok"}), "luma-ray-3.2 is conditional: not commercial OK")
	assert.Len(t, pick(export.Selection{Commercial: "ok_or_conditional"}), 4)
	assert.Len(t, pick(export.Selection{Commercial: "all"}), 4)
	assert.Len(t, pick(export.Selection{}), 4, "no commercial condition leaves everything in: an export is not narrowed by the setting")

	for name, sel := range map[string]export.Selection{
		"media":      {Media: "audio"},
		"commercial": {Commercial: "maybe"},
		"latest":     {Latest: -1},
		"too many":   {Latest: export.MaxItems + 1},
		"bad id":     {ResultIDs: []string{"a/b"}},
		"sql id":     {ResultIDs: []string{"x') OR 1=1 --"}},
		"only blank": {ResultIDs: []string{" "}},
	} {
		_, err := ex.Plan(ctx, sel, export.Split{})
		var invalid *gterr.InvalidError
		assert.ErrorAs(t, err, &invalid, name)
	}
	_, err := ex.Plan(ctx, export.Selection{BatchID: "no-such-batch"}, export.Split{})
	assert.Error(t, err, "a batch that does not exist has nothing to export")
	_, err = ex.Plan(ctx, export.Selection{}, export.Split{Items: -1})
	assert.Error(t, err)
}

func TestFailedResultsAndMissingFilesAreNotExported(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	res := images(t, r, "flux-2", "fox", "cat")
	file, err := r.Svc.ResultFile(ctx, res[0].ID)
	require.NoError(t, err)
	require.NoError(t, removeFile(file.Path))

	p, err := export.New(r.Svc).Plan(ctx, export.Selection{}, export.Split{})
	require.NoError(t, err)
	assert.Equal(t, []string{res[1].ID}, idsOf(p.Items), "a result whose file is gone is left out")
}

func TestSplitKeepsTheNumbersOfTheWholeAndEachPartOpensAgain(t *testing.T) {
	r := gentesttest.New(t)
	ctx := context.Background()
	ex := export.New(r.Svc)
	res := images(t, r, "flux-2", "p1", "p2", "p3", "p4", "p5")

	p, err := ex.Plan(ctx, export.Selection{BatchID: res[0].BatchID}, export.Split{Items: 2})
	require.NoError(t, err)
	require.Len(t, p.Parts, 3)
	assert.Equal(t, []int{2, 2, 1}, []int{p.Parts[0].Items, p.Parts[1].Items, p.Parts[2].Items})
	assert.Equal(t, []int{1, 3, 5}, []int{p.Parts[0].First, p.Parts[1].First, p.Parts[2].First})
	assert.Equal(t, []int{2, 4, 5}, []int{p.Parts[0].Last, p.Parts[1].Last, p.Parts[2].Last})
	assert.Regexp(t, `_part2of3\.zip$`, p.Parts[1].Name)
	assert.Equal(t, int64(len(gentesttest.PNG))*2, p.Parts[0].Bytes)

	seen := map[string]bool{}
	for _, part := range p.Parts {
		parsed, err := export.ParseRef(part.Ref.String())
		require.NoError(t, err, "a ref survives being a ticket's resource id")
		arc, err := ex.Open(ctx, parsed)
		require.NoError(t, err)
		assert.Equal(t, part.Name, arc.Name)
		_, files := unzip(t, arc)
		for _, row := range sheet(t, files) {
			assert.False(t, seen[row[0]], "a file is in one part only")
			seen[row[0]] = true
			assert.Contains(t, files, export.PhotosDir+row[0])
		}
	}
	assert.Len(t, seen, 5)
	for n := 1; n <= 5; n++ {
		found := false
		for f := range seen {
			found = found || strings.HasPrefix(f, "0"+string(rune('0'+n))+"_")
		}
		assert.True(t, found, "number %d is in some part: the parts add up to numbers 1..5", n)
	}

	bySize, err := ex.Plan(ctx, export.Selection{BatchID: res[0].BatchID}, export.Split{Bytes: int64(len(gentesttest.PNG)) * 2})
	require.NoError(t, err)
	assert.Len(t, bySize.Parts, 3, "two files fit, the third starts the next ZIP")
	tiny, err := ex.Plan(ctx, export.Selection{BatchID: res[0].BatchID}, export.Split{Bytes: 1})
	require.NoError(t, err)
	assert.Len(t, tiny.Parts, 5, "a file bigger than the limit is a ZIP of its own")
}

func TestARefThatIsNotOneOfOursIsRefused(t *testing.T) {
	for _, s := range []string{"", "x", "1/1/1/1", "0/1/1/1/a", "2/1/1/1/a", "1/1/1/1/", "1/1/1/1/a/b", "1/1/1/1/a b", "1/99/1/1/a", "1/1/1/1/../x"} {
		_, err := export.ParseRef(s)
		assert.Error(t, err, "%q", s)
	}
	ref, err := export.ParseRef("2/3/21/50/abc,def")
	require.NoError(t, err)
	assert.Equal(t, export.Ref{Part: 2, Parts: 3, First: 21, Total: 50, IDs: []string{"abc", "def"}}, ref)
	assert.Equal(t, "2/3/21/50/abc,def", ref.String())
}

func TestWriteStopsWithAnErrorWhenTheConnectionBreaks(t *testing.T) {
	r := gentesttest.New(t)
	res := images(t, r, "flux-2", "fox")
	arc, err := export.New(r.Svc).Archive(context.Background(), export.Selection{BatchID: res[0].BatchID})
	require.NoError(t, err)
	assert.Error(t, arc.Write(failAfter{n: 10}), "a write error is returned, not swallowed")
}

func TestAVideoIsExportedWithItsExtensionAndMedia(t *testing.T) {
	r := gentesttest.New(t)
	clip := video(t, r, "runway-gen-4.5", "waves")
	arc, err := export.New(r.Svc).Archive(context.Background(), export.Selection{ResultIDs: []string{clip.ID}})
	require.NoError(t, err)
	_, files := unzip(t, arc)
	rows := sheet(t, files)
	require.Len(t, rows, 1)
	assert.Regexp(t, `^01_\d{8}-\d{6}_runway-gen-4\.5_[0-9a-f]{6}\.mp4$`, rows[0][0])
	assert.Equal(t, "video", rows[0][2])
	assert.Equal(t, []byte("MP4DATA"), files[export.PhotosDir+rows[0][0]])
}
