package refform

import (
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/service/gentest"
	view "github.com/ousiassllc/mam/backend/internal/web/organisms/gentest"
)

func file(name string, size int64) *multipart.FileHeader {
	return &multipart.FileHeader{Filename: name, Size: size}
}

// stageName stores nothing: the id of a file is "id-<name>", so a test can tell which file an entry got.
func stageName(fh *multipart.FileHeader) (string, error) { return "id-" + fh.Filename, nil }

func placeholderEntry(n string, role string, strength float64) gentest.RefInput {
	return gentest.RefInput{Source: gentest.FromStaged, ID: "#" + n, Role: role, Strength: &strength}
}

func stage(t *testing.T, refs []gentest.RefInput, uploads ...*multipart.FileHeader) []gentest.RefInput {
	t.Helper()
	out, err := Stage(refs, &multipart.Form{File: map[string][]*multipart.FileHeader{view.FieldUpload: uploads}}, stageName)
	require.NoError(t, err)
	return out
}

// An empty file keeps its number: "#1" (empty) is dropped with its role, and "#2" still gets the second file, not the first.
func TestStageKeepsThePositionOfAnEmptyFile(t *testing.T) {
	out := stage(t,
		[]gentest.RefInput{placeholderEntry("1", gentest.RoleFace, 0.2), placeholderEntry("2", gentest.RoleStyle, 0.9)},
		file("empty.png", 0), file("b.png", 10))
	require.Len(t, out, 1)
	assert.Equal(t, "id-b.png", out[0].ID)
	assert.Equal(t, gentest.RoleStyle, out[0].Role, "b.png keeps the role of its own entry")
	assert.InDelta(t, 0.9, *out[0].Strength, 0)
}

// A file in the middle that is empty does not shift the ones after it, and a file no entry names is not taken from the wrong slot.
func TestStageEmptyFileInTheMiddle(t *testing.T) {
	out := stage(t,
		[]gentest.RefInput{placeholderEntry("3", gentest.RoleEdit, 0.5), placeholderEntry("1", gentest.RoleFace, 0.1)},
		file("a.png", 5), file("empty.png", 0), file("c.png", 5))
	require.Len(t, out, 2)
	assert.Equal(t, "id-c.png", out[0].ID)
	assert.Equal(t, "id-a.png", out[1].ID)
}

func TestStageDropsEntriesWithoutAFile(t *testing.T) {
	for name, entry := range map[string]gentest.RefInput{
		"zero":         placeholderEntry("0", gentest.RoleFace, 0.5),
		"out of range": placeholderEntry("3", gentest.RoleFace, 0.5),
		"negative":     placeholderEntry("-1", gentest.RoleFace, 0.5),
		"not a number": placeholderEntry("abc", gentest.RoleFace, 0.5),
		"no number":    placeholderEntry("", gentest.RoleFace, 0.5),
	} {
		out := stage(t, []gentest.RefInput{entry}, file("a.png", 5), file("b.png", 5))
		// the two files that no entry names are added as free references; the entry itself is not kept as it is
		require.Len(t, out, 2, name)
		assert.Equal(t, []string{"id-a.png", "id-b.png"}, []string{out[0].ID, out[1].ID}, name)
		assert.Equal(t, gentest.RoleReference, out[0].Role, name)
	}
}

// Two entries that name the same file: the file is in one place only (the first); the second is dropped.
func TestStageDuplicateNumberTakesTheFileOnce(t *testing.T) {
	out := stage(t,
		[]gentest.RefInput{placeholderEntry("1", gentest.RoleFace, 0.2), placeholderEntry("1", gentest.RoleStyle, 0.8)},
		file("a.png", 5))
	require.Len(t, out, 1)
	assert.Equal(t, "id-a.png", out[0].ID)
	assert.Equal(t, gentest.RoleFace, out[0].Role)
}

func TestStageEmptyFramesAreSkipped(t *testing.T) {
	form := &multipart.Form{File: map[string][]*multipart.FileHeader{
		view.FieldFirst: {file("f0.png", 0)}, view.FieldLast: {file("l.png", 3)},
	}}
	out, err := Stage(nil, form, stageName)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, gentest.RoleLastFrame, out[0].Role)
}

func TestStrengthOf(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
	}{
		{"0", 0}, {"0.35", 0.35}, {"1", 1}, {"2", 1}, {"-3", 0}, {" 0.5 ", 0.5},
		{"Inf", 1}, {"+Inf", 1}, {"-Inf", 0}, {"1e999", 1}, {"-1e999", 0},
	} {
		got := strengthOf(c.in)
		require.NotNil(t, got, c.in)
		assert.InDelta(t, c.want, *got, 0, c.in)
	}
	for _, in := range []string{"", "abc", "NaN", "0,5"} {
		assert.Nil(t, strengthOf(in), "%q is not a number: the default is used", in)
	}
}
