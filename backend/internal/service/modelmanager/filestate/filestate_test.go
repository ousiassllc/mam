package filestate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/filestate"
)

func report(files map[catalog.File]bool) filestate.Report {
	var r filestate.Report
	for f, present := range files {
		r.Add(f, present)
	}
	return r
}

var (
	diffusion = catalog.File{Name: "d.safetensors"}
	lora      = catalog.File{Name: "l.safetensors", Optional: true}
)

func TestAnOptionalFileMissingKeepsTheModelInstalledAndIsNamed(t *testing.T) {
	r := report(map[catalog.File]bool{diffusion: true, lora: false})
	assert.True(t, r.RequiredInPlace())
	r.Seal(true, true)
	assert.Equal(t, []string{"l.safetensors"}, r.MissingOptional)
	assert.Contains(t, r.PartialNote, "l.safetensors")
	assert.False(t, r.FilesInPlace)
}

func TestARequiredFileMissingIsNotInPlaceAndSaysNothingOfTheOptionalOnes(t *testing.T) {
	r := report(map[catalog.File]bool{diffusion: false, lora: false})
	assert.False(t, r.RequiredInPlace())
	r.Seal(false, true) // recorded, but incomplete
	assert.Empty(t, r.MissingOptional)
	assert.Empty(t, r.PartialNote)
	assert.False(t, r.FilesInPlace)
}

func TestFilesInPlaceWithoutARecordAreSaid(t *testing.T) {
	r := report(map[catalog.File]bool{diffusion: true, lora: false})
	r.Seal(false, false)
	assert.True(t, r.FilesInPlace)
	assert.Empty(t, r.MissingOptional)

	r = report(map[catalog.File]bool{diffusion: false})
	r.Seal(false, false)
	assert.False(t, r.FilesInPlace, "a required file is missing as well")
}

func TestAFullyInstalledModelHasNoNote(t *testing.T) {
	r := report(map[catalog.File]bool{diffusion: true, lora: true})
	r.Seal(true, true)
	assert.Empty(t, r.MissingOptional)
	assert.Empty(t, r.PartialNote)
}
