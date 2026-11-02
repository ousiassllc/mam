package facesim_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/facesim"
)

// TestONNX_FaceLockPairs prints the face similarity of the pairs of a measurement of the "face only" role (Issue #960) with the
// metric mam has (SFace, Apache-2.0), beside the metric of the earlier measurement (InsightFace buffalo_l, non-commercial: not part of
// mam, so its numbers come in as a file), over the same pictures. It asserts nothing: its output is the table of
// spec/architecture/local-models.md §4.5 ("how to measure"). It needs the models and the library of the tests above and
//
//	MAM_FACESIM_PAIRS  a directory with, for each pair <name>: <name>.ref.<png|jpg|jpeg|webp> (the reference of the run),
//	                   <name>.out.<same> (the picture the model made from it) and, optionally, <name>.old with the number the
//	                   earlier metric gave for the pair (a text file with one number)
//
// The columns: the similarity of the whole reference to the output, of the reference cut to its face (facesim.CropFace) to the output,
// the old number, and the mean similarity of the reference to the outputs of the other pairs ("another person": what a number has to
// stand out from). A pair with no face on a side is marked "-".
func TestONNX_FaceLockPairs(t *testing.T) {
	dir := os.Getenv("MAM_FACESIM_PAIRS")
	if dir == "" {
		t.Skip("MAM_FACESIM_PAIRS is not set")
	}
	e := newEngine(t, modelDirFromEnv(t))
	ctx := context.Background()

	names := pairNames(t, dir)
	require.NotEmpty(t, names, "no <name>.ref.* in %s", dir)
	refs := map[string][]byte{}
	outs := map[string]image.Image{}
	for _, n := range names {
		refs[n] = readPair(t, dir, n, "ref")
		outs[n] = decodePair(t, readPair(t, dir, n, "out"))
	}
	cosine := func(ref []byte, out image.Image) string {
		v, side, err := facesim.Similarity(ctx, e, decodePair(t, ref), out)
		require.NoError(t, err)
		if side != facesim.BothHaveFaces {
			return "-"
		}
		return fmt.Sprintf("%.3f", v)
	}

	var rows []string
	for _, n := range names {
		cut, ok, err := facesim.CropFace(ctx, e, refs[n])
		require.NoError(t, err)
		var cropped string
		if ok {
			cropped = cosine(cut, outs[n])
		} else {
			cropped = cosine(refs[n], outs[n]) + " (not cut)"
		}
		old := "-"
		if raw, err := os.ReadFile(filepath.Join(dir, n+".old")); err == nil { //nolint:gosec // a path of the measurement's own directory
			old = strings.TrimSpace(string(raw))
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s | %s |", n, cosine(refs[n], outs[n]), cropped, old, otherMean(ctx, t, e, names, refs, outs, n)))
	}
	t.Logf("\n| pair | SFace whole ref | SFace face-cut ref | earlier metric | SFace ref vs other pairs' outputs |\n|---|---|---|---|---|\n%s", strings.Join(rows, "\n"))
}

// otherMean is the mean similarity of the reference of name to the outputs of the other pairs ("-" with fewer than two pairs).
func otherMean(ctx context.Context, t *testing.T, e facesim.Engine, names []string, refs map[string][]byte, outs map[string]image.Image, name string) string {
	t.Helper()
	var sum float64
	var n int
	ref := decodePair(t, refs[name])
	for _, other := range names {
		if other == name {
			continue
		}
		v, side, err := facesim.Similarity(ctx, e, ref, outs[other])
		require.NoError(t, err)
		if side == facesim.BothHaveFaces {
			sum += v
			n++
		}
	}
	if n == 0 {
		return "-"
	}
	return strconv.FormatFloat(sum/float64(n), 'f', 3, 64)
}

func pairNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, en := range entries {
		if n, _, ok := strings.Cut(en.Name(), ".ref."); ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// readPair reads <name>.<kind>.<ext> whatever the extension is.
func readPair(t *testing.T, dir, name, kind string) []byte {
	t.Helper()
	for _, ext := range []string{"png", "jpg", "jpeg", "webp"} {
		if data, err := os.ReadFile(filepath.Join(dir, name+"."+kind+"."+ext)); err == nil { //nolint:gosec // a path of the measurement's own directory
			return data
		}
	}
	require.FailNow(t, "no picture", "%s.%s.(png|jpg|jpeg|webp) in %s", name, kind, dir)
	return nil
}

func decodePair(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return img
}
