package mmview_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/apperr"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/fetch"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager"
	"github.com/ousiassllc/mam/backend/internal/service/modelmanager/mmview"
)

// worded is an install failure the installer put in words itself (modelmanager's userError), over the failure behind it.
type worded struct {
	msg string
	err error
}

func (e *worded) Error() string       { return e.msg }
func (e *worded) UserMessage() string { return e.msg }
func (e *worded) Unwrap() error       { return e.err }

// An install the installer worded keeps its sentence over a vendor's code: the source of a file that answered 404 is not "the URL of the
// endpoint is wrong". The machine's own failures (the disk) still get their code, with a remedy that fits.
func TestFailure_AnInstallTheInstallerWordedKeepsItsSentenceOverAVendorCode(t *testing.T) {
	gone := &worded{"取得元が消えています（extra、HTTP 404）。", fmt.Errorf("%w: %w", modelmanager.ErrSourceGone, &fetch.HTTPError{Status: 404, URL: "https://x/extra"})}
	e := mmview.Failure(gone)
	require.NotNil(t, e)
	assert.Equal(t, apperr.CodeInstallFailed, e.Code)
	assert.Equal(t, "取得元が消えています（extra、HTTP 404）。", e.View(apperr.LangJa).Cause)
	assert.Contains(t, e.Detail, "404", "the machine's words are the detail")

	full := mmview.Failure(&worded{"空き容量が足りません", &fetch.NoSpaceError{Need: 10, Free: 1, Path: "/models"}})
	require.NotNil(t, full)
	assert.Equal(t, apperr.CodeDiskFull, full.Code)

	bare := mmview.Failure(&fetch.HTTPError{Status: 404, URL: "https://x/extra"})
	require.NotNil(t, bare)
	assert.Equal(t, apperr.CodeRemoteNotFound, bare.Code, "a failure nobody worded is classified")
}
