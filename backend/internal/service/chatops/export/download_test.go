package export_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/commandinterpreter"
	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/export"
)

func TestDownload_AnswersWithTheLatestExportsLinkAndChangesNothing(t *testing.T) {
	v := ready()
	old := domain.Export{ID: "e1", AspectRatio: "16:9", Resolution: "1080p", Codec: "h264", ExportedAt: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)}
	newest := domain.Export{ID: "e2", AspectRatio: "9:16", Resolution: "720p", Codec: "h264", ExportedAt: time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)}
	v.Exports = []domain.Export{old, newest}
	f := &fakeEditor{view: v}

	res, err := export.NewDownload(f.deps()).Apply(context.Background(), project, commandinterpreter.Operation{Type: export.OpDownloadExport})

	require.NoError(t, err)
	assert.Contains(t, res.Text, "9:16 / 720p / h264")
	require.Len(t, res.Messages, 1)
	notice := res.Messages[0]
	assert.Equal(t, domain.RefExport, notice.RefType, "the chat draws the download button of an export notice")
	assert.Equal(t, "e2", notice.RefID)
	assert.Equal(t, domain.KindText, notice.Kind)
	assert.Empty(t, f.exported)
}

func TestDownload_WithoutAnExportSaysSoAndOffersToMakeOne(t *testing.T) {
	f := &fakeEditor{view: ready()}

	res, err := export.NewDownload(f.deps()).Apply(context.Background(), project, commandinterpreter.Operation{Type: export.OpDownloadExport})

	require.NoError(t, err)
	assert.Contains(t, res.Text, "まだ書き出した動画がありません")
	assert.Contains(t, res.Text, "書き出して")
	assert.Empty(t, res.Messages)
}

func TestDownload_NeverAsks(t *testing.T) {
	_, err := export.NewDownload(export.Deps{}).Question(context.Background(), project, commandinterpreter.Operation{})
	assert.ErrorIs(t, err, chatops.ErrNoChange)
	_, err = export.Bundle{}.Question(context.Background(), project, commandinterpreter.Operation{})
	assert.ErrorIs(t, err, chatops.ErrNoChange)
}

func TestBundle_AnswersWithTheRouteOfTheBundle(t *testing.T) {
	res, err := export.Bundle{}.Apply(context.Background(), domain.Project{ID: "p 1"}, commandinterpreter.Operation{Type: export.OpExportBundle})

	require.NoError(t, err)
	assert.Contains(t, res.Text, "/projects/p%201/bundle")
	assert.Contains(t, res.Text, "ZIP")
}
