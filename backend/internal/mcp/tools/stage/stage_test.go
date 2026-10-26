package stage

import (
	"io"
	"mime/multipart"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/mcp/mcpkit"
	"github.com/ousiassllc/mam/backend/internal/service"
)

func header(t *testing.T, name, content string) *multipart.FileHeader {
	t.Helper()
	var sb strings.Builder
	w := multipart.NewWriter(&sb)
	part, err := w.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="file"; filename="` + name + `"`}})
	require.NoError(t, err)
	_, err = io.WriteString(part, content)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	form, err := multipart.NewReader(strings.NewReader(sb.String()), w.Boundary()).ReadForm(1 << 20)
	require.NoError(t, err)
	return form.File["file"][0]
}

func TestStore_ClaimTakesAFileOnceAndOnlyForItsOwner(t *testing.T) {
	s := New()
	f, err := s.Add("p1", mcpkit.UploadInput{Header: header(t, "../dir/a.png", "image-bytes")})
	require.NoError(t, err)
	assert.Equal(t, "a.png", f.Name, "the name is a base name")
	assert.EqualValues(t, len("image-bytes"), f.SizeBytes)

	_, err = s.Claim("p2", []string{f.ID})
	var invalid *service.ValidationError
	require.ErrorAs(t, err, &invalid, "another Project cannot use the file")
	assert.Equal(t, Field, invalid.Field)
	_, err = s.Claim("", []string{f.ID})
	require.Error(t, err, "nor a new Project")

	claimed, err := s.Claim("p1", []string{f.ID, f.ID})
	require.NoError(t, err)
	require.Len(t, claimed, 1, "a repeated id counts once")
	rc, err := Uploads(claimed)[0].Open()
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, "image-bytes", string(got))

	_, err = s.Claim("p1", []string{f.ID})
	require.ErrorAs(t, err, &invalid, "a claimed file is gone")
}

func TestStore_ClaimTakesNothingWhenOneIDIsBad_ReleaseGivesTheFilesBack(t *testing.T) {
	s := New()
	a, err := s.Add("p1", mcpkit.UploadInput{Header: header(t, "a.png", "a")})
	require.NoError(t, err)

	_, err = s.Claim("p1", []string{a.ID, "nope"})
	require.Error(t, err)
	claimed, err := s.Claim("p1", []string{a.ID})
	require.NoError(t, err, "the good id was not taken by the failed claim")

	s.Release(claimed)
	claimed, err = s.Claim("p1", []string{a.ID})
	require.NoError(t, err, "released: the call can be retried")

	s.Discard(claimed)
	_, statErr := os.Stat(claimed[0].path)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a used file is deleted")
}

func TestStore_ExpiredFilesAreDeletedAndRefused(t *testing.T) {
	s := New()
	now := time.Now()
	s.now = func() time.Time { return now }
	f, err := s.Add("p1", mcpkit.UploadInput{Header: header(t, "a.png", "a")})
	require.NoError(t, err)

	now = now.Add(mcpkit.TicketTTL + time.Second)
	_, err = s.Claim("p1", []string{f.ID})
	require.Error(t, err)
	_, statErr := os.Stat(f.path)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}
