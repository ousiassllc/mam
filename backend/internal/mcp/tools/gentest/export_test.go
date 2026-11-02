package gentest_test

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit/mcptest"
	mcpgentest "github.com/ousiassllc/mam/backend/internal/mcp/tools/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	"github.com/ousiassllc/mam/backend/internal/service/gentest/gentesttest"
)

// finished runs the batch (2 prompts x 2 each) and returns its results once it is over.
func (r *rig) finished(models ...string) gentest.BatchStatus {
	r.T.Helper()
	out, _ := r.run(batch(models...))
	require.Equal(r.T, mcpkit.JobSucceeded, r.job(out.JobID).State)
	var b gentest.BatchStatus
	r.CallOK("get_generation_test", map[string]any{"batch_id": out.BatchID}, &b)
	return b
}

// download fetches a ticket URL of the export and opens the ZIP.
func (r *rig) download(url string) (map[string][]byte, *httptest.ResponseRecorder) {
	r.T.Helper()
	rec := r.serve(http.MethodGet, url, nil, "")
	require.Equal(r.T, http.StatusOK, rec.Code)
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	require.NoError(r.T, err)
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(r.T, err)
		data, err := io.ReadAll(rc)
		require.NoError(r.T, err)
		_ = rc.Close()
		files[f.Name] = data
	}
	return files, rec
}

func csvRows(t *testing.T, files map[string][]byte) [][]string {
	t.Helper()
	raw := files["prompts/prompts.csv"]
	require.True(t, bytes.HasPrefix(raw, []byte("\xEF\xBB\xBF")))
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF")))).ReadAll()
	require.NoError(t, err)
	return rows[1:]
}

func TestExportGivesATicketUrlOfAZipWithPhotosAndPrompts(t *testing.T) {
	r := newRig(t)
	b := r.finished("flux-2", "nano-banana")
	require.Len(t, b.Results, 8)

	var out mcpgentest.ExportOutput
	r.CallOK("export_generation_results", map[string]any{"latest": 5}, &out)
	require.Len(t, out.Files, 1)
	require.Len(t, out.Items, 5)
	f := out.Files[0]
	assert.Contains(t, f.URL, mcpkit.FilesPath, "a ticket URL, not the bytes")
	assert.False(t, f.ExpiresAt.IsZero(), "it expires")
	assert.Regexp(t, `^generation-results_\d{8}-\d{6}_5\.zip$`, f.FileName)
	assert.Equal(t, 5, f.Items)
	assert.Equal(t, []int{1, 5}, []int{f.First, f.Last})

	files, rec := r.download(f.URL)
	assert.Equal(t, "application/zip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), f.FileName)
	rows := csvRows(t, files)
	require.Len(t, rows, 5)
	for i, row := range rows {
		assert.Equal(t, out.Items[i].File, row[0], "the output, the sheet and photos/ agree")
		assert.Equal(t, out.Items[i].ResultID, row[7])
		assert.Equal(t, gentesttest.PNG, files["photos/"+row[0]])
	}
	assert.Len(t, files, 6)
}

func TestExportSelectsByBatchFavoriteModelMediaAndCommercialUse(t *testing.T) {
	r := newRig(t)
	flux := r.finished("flux-2")
	banana := r.finished("nano-banana")
	r.CallOK("set_generation_favorite", map[string]any{"result_id": banana.Results[1].ID, "favorite": true}, &mcpgentest.FavoriteOutput{})

	count := func(args map[string]any) int {
		var out mcpgentest.ExportOutput
		r.CallOK("export_generation_results", args, &out)
		return len(out.Items)
	}
	assert.Equal(t, 4, count(map[string]any{"batch_id": flux.BatchID}))
	assert.Equal(t, 1, count(map[string]any{"favorite": true}))
	assert.Equal(t, 4, count(map[string]any{"models": []string{"nano-banana"}}))
	assert.Equal(t, 8, count(map[string]any{"media": "image", "commercial": "ok"}))
	assert.Equal(t, 2, count(map[string]any{"result_ids": []string{flux.Results[0].ID, banana.Results[0].ID}}))
	assert.Equal(t, 8, count(map[string]any{}), "nothing said: the newest 40")

	var out mcpgentest.ExportOutput
	r.CallOK("export_generation_results", map[string]any{"result_ids": []string{flux.Results[0].ID, "nope"}}, &out)
	assert.Equal(t, []string{"nope"}, out.Skipped)

	for name, args := range map[string]map[string]any{
		"media":      {"media": "audio"},
		"commercial": {"commercial": "maybe"},
		"latest":     {"latest": 501},
		"split":      {"split_items": -1},
		"bad id":     {"result_ids": []string{"../etc/passwd"}},
		"no results": {"batch_id": "no-such-batch"},
	} {
		assert.Contains(t, r.CallError("export_generation_results", args), "validation", name)
	}
}

func TestExportSplitsIntoSeveralZipsThatKeepTheNumbers(t *testing.T) {
	r := newRig(t)
	r.finished("flux-2")

	var out mcpgentest.ExportOutput
	r.CallOK("export_generation_results", map[string]any{"split_items": 3}, &out)
	require.Len(t, out.Files, 2)
	assert.Equal(t, []int{3, 1}, []int{out.Files[0].Items, out.Files[1].Items})
	assert.Equal(t, []int{4, 4}, []int{out.Files[1].First, out.Files[1].Last})
	assert.Regexp(t, `_part2of2\.zip$`, out.Files[1].FileName)
	second, _ := r.download(out.Files[1].URL)
	rows := csvRows(t, second)
	require.Len(t, rows, 1)
	assert.True(t, strings.HasPrefix(rows[0][0], "04_"), "the second ZIP goes on counting: %s", rows[0][0])

	var bySize mcpgentest.ExportOutput
	r.CallOK("export_generation_results", map[string]any{"split_mb": 1}, &bySize)
	assert.Len(t, bySize.Files, 1, "four 1x1 PNGs are far below 1 MB")
	assert.Positive(t, bySize.Files[0].Bytes)
}

func TestAnExportUrlServesOnlyWhatWasChosenAndOnlyToItsHolder(t *testing.T) {
	r := newRig(t)
	b := r.finished("flux-2")
	var out mcpgentest.ExportOutput
	r.CallOK("export_generation_results", map[string]any{"result_ids": []string{b.Results[0].ID}}, &out)
	link := out.Files[0].URL

	// A guess is a 404 as any other wrong ticket (the URL carries a random value, not the choice).
	for _, bad := range []string{link[:len(link)-1], link + "x", link[:strings.LastIndex(link, "/")+1] + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		assert.Equal(t, http.StatusNotFound, r.serve(http.MethodGet, bad, nil, "").Code, bad)
	}
	// It is a download ticket: it cannot be used to upload.
	body, ct := multipartBody(t, mcpkit.FileField, "x.png", gentesttest.PNG)
	assert.Equal(t, http.StatusNotFound, r.serve(http.MethodPost, link, body, ct).Code)
	// Its ZIP holds the one result that was chosen, however the history changes.
	r.finished("nano-banana")
	files, _ := r.download(link)
	assert.Len(t, csvRows(t, files), 1)

	// A result deleted after the URL was issued is left out; with nothing left the URL is a 404.
	r.CallOK("delete_generation_result", map[string]any{"result_id": b.Results[0].ID}, &mcpgentest.DeletedOutput{})
	assert.Equal(t, http.StatusNotFound, r.serve(http.MethodGet, link, nil, "").Code)
}

func TestExportIsUnavailableWithoutAGenerationTest(t *testing.T) {
	env := mcptest.New(t, func(env *mcptest.Env, reg *mcpkit.Registrar) { mcpgentest.Register(reg, env.Services) })
	assert.Contains(t, env.CallError("export_generation_results", map[string]any{}), "unavailable")
}
