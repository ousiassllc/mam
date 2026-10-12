package bundlezip

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const limitsProjectID = "p1"

// zipOf builds a bundle ZIP of the manifest and the data.json that writeData writes (deflated as
// fast as possible: the data is highly repetitive, so the ZIP stays small).
func zipOf(t *testing.T, manifest string, writeData func(w io.Writer)) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { return flate.NewWriter(w, flate.BestSpeed) })
	mw, err := zw.Create("manifest.json")
	require.NoError(t, err)
	_, err = io.WriteString(mw, manifest)
	require.NoError(t, err)
	dw, err := zw.Create("data.json")
	require.NoError(t, err)
	writeData(dw)
	require.NoError(t, zw.Close())
	return bytes.NewReader(buf.Bytes())
}

func manifestJSON() string {
	return fmt.Sprintf(`{"format":"mam-project-bundle","version":1,"project_id":%q,"exported_at":"2026-01-01T00:00:00Z"}`, limitsProjectID)
}

// rowsData writes a data.json whose one table has n rows of row.
func rowsData(n int, row string) func(io.Writer) {
	return func(w io.Writer) {
		_, _ = fmt.Fprintf(w, `{"project_id":%q,"tables":[{"name":"chat_message","rows":[`, limitsProjectID)
		for i := range n {
			if i > 0 {
				_, _ = io.WriteString(w, ",")
			}
			_, _ = io.WriteString(w, row)
		}
		_, _ = io.WriteString(w, "]}]}")
	}
}

func openBytes(t *testing.T, r *bytes.Reader) (*Archive, error) {
	t.Helper()
	return Open(r, r.Size())
}

func requireInvalid(t *testing.T, err error) {
	t.Helper()
	var inv *InvalidError
	require.ErrorAs(t, err, &inv)
}

func TestOpen_DecodesDataRowByRow(t *testing.T) {
	src := zipOf(t, manifestJSON(), func(w io.Writer) {
		_, _ = io.WriteString(w, `{"unknown":{"a":[1,{"b":null}]},"project_id":"p1","tables":[`+
			`{"name":"project","extra":[1,2],"rows":[{"id":"p1","n":12,"f":1.5,"x":null,"b":true}]},`+
			`{"name":"scene","rows":null}]}`)
	})

	a, err := openBytes(t, src)

	require.NoError(t, err)
	assert.Equal(t, limitsProjectID, a.Data.ProjectID)
	require.Len(t, a.Data.Tables, 2)
	assert.Equal(t, "project", a.Data.Tables[0].Name)
	assert.Equal(t, []map[string]any{{"id": "p1", "n": json.Number("12"), "f": json.Number("1.5"), "x": nil, "b": true}},
		[]map[string]any{a.Data.Tables[0].Rows[0]})
	assert.Equal(t, "scene", a.Data.Tables[1].Name)
	assert.Empty(t, a.Data.Tables[1].Rows)
}

// TestOpen_RowLimitStopsTheDecode is the memory regression: a data.json whose rows are tiny and
// repeat is a few KB of ZIP per million rows but a map[string]any each once decoded. The decode must
// stop at maxRows whatever follows, so the heap it allocates does not grow with the size of data.json.
func TestOpen_RowLimitStopsTheDecode(t *testing.T) {
	allocated := func(rows int) uint64 {
		src := zipOf(t, manifestJSON(), rowsData(rows, `{"id":"a"}`))
		t.Logf("%d rows: ZIP %d bytes", rows, src.Size())
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		_, err := openBytes(t, src)
		runtime.ReadMemStats(&after)
		requireInvalid(t, err)
		assert.Contains(t, err.Error(), "上限")
		return after.TotalAlloc - before.TotalAlloc
	}

	atLimit := allocated(maxRows + 1)
	fourTimes := allocated(4*maxRows + 1)

	assert.Less(t, float64(fourTimes), 1.5*float64(atLimit), "decoding 4x the rows must not allocate 4x the memory")
}

func TestOpen_AcceptsExactlyMaxRows(t *testing.T) {
	a, err := openBytes(t, zipOf(t, manifestJSON(), rowsData(maxRows, `{"id":"a"}`)))

	require.NoError(t, err)
	assert.Len(t, a.Data.Tables[0].Rows, maxRows)
}

func TestOpen_RefusesWhatCostsMoreMemoryThanARealBundle(t *testing.T) {
	rowOf := func(columns int) string {
		cols := make([]string, columns)
		for i := range cols {
			cols[i] = fmt.Sprintf(`"c%d":1`, i)
		}
		return "{" + strings.Join(cols, ",") + "}"
	}
	cases := map[string]func(io.Writer){
		"a row with too many columns": rowsData(1, rowOf(maxRowColumns+1)),
		"an array in a row":           rowsData(1, `{"id":[1,2]}`),
		"an object in a row":          rowsData(1, `{"id":{"a":1}}`),
		"a row that is not an object": rowsData(1, `"a"`),
		"too many tables": func(w io.Writer) {
			_, _ = fmt.Fprintf(w, `{"project_id":%q,"tables":[`, limitsProjectID)
			for i := range maxTables + 1 {
				if i > 0 {
					_, _ = io.WriteString(w, ",")
				}
				_, _ = fmt.Fprintf(w, `{"name":"t%d","rows":[]}`, i)
			}
			_, _ = io.WriteString(w, "]}")
		},
		"too many values in all rows": rowsData(maxCells/maxRowColumns+1, rowOf(maxRowColumns)),
	}
	for name, writeData := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := openBytes(t, zipOf(t, manifestJSON(), writeData))
			requireInvalid(t, err)
		})
	}
}

func TestOpen_ManifestIsHeldToItsOwnSmallLimit(t *testing.T) {
	padded := strings.TrimSuffix(manifestJSON(), "}") + `,"pad":"` + strings.Repeat("x", maxManifest) + `"}`

	_, err := openBytes(t, zipOf(t, padded, rowsData(1, `{"id":"a"}`)))

	requireInvalid(t, err)
	assert.Contains(t, err.Error(), "manifest.json が大きすぎます")
}
