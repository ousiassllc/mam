package chattest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/domain"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/chatfix"
	"github.com/ousiassllc/mam/backend/internal/service/chatops/export"
	"github.com/ousiassllc/mam/backend/internal/service/chatwire"
)

// exportFake stands in for EditorService.Export (which needs ffmpeg): it records the calls and moves the Project to EXPORTED the way the service does.
type exportFake struct {
	state  domain.ProjectState
	called []export.Args
}

func (e *exportFake) deps() export.Deps {
	return export.Deps{
		View: func(context.Context, string) (export.View, error) {
			return export.View{
				State: e.state, CanRender: true, HasAudio: true, AudioCurrent: true,
				Defaults: domain.ExportOptions{AspectRatio: "16:9", Resolution: "1080p", Codec: "h264"},
			}, nil
		},
		Export: func(_ context.Context, _ string, in export.Args) (export.Done, error) {
			e.called = append(e.called, in)
			return export.Done{ExportID: "e-new", State: domain.StateExported, Options: domain.ExportOptions{AspectRatio: in.AspectRatio, Resolution: in.Resolution, Codec: in.Codec}}, nil
		},
	}
}

// newExportFixture is a fixture whose export operations are wired as the app wires them (chatwire.Connect), with the render itself replaced by a fake, on a READY Project.
func newExportFixture(t *testing.T) (*chatfix.Fixture, *exportFake) {
	t.Helper()
	f := chatfix.New(t, chatfix.Wired(chatwire.Settings{}))
	fake := &exportFake{state: domain.StateReady}
	f.Svc.ChatOps().Handle(export.OpExportVideo, export.NewRender(fake.deps()))
	f.SQL(`UPDATE project SET state = 'READY' WHERE id = ?`, f.ProjectID)
	return f, fake
}

// insertExport adds an export made on the given day (YYYY-MM-DD): the latest day is the latest export.
func insertExport(f *chatfix.Fixture, id, day, ratio, resolution string) {
	f.T.Helper()
	f.SQL(`INSERT INTO export (id, project_id, file_path, resolution, aspect_ratio, codec, exported_at) VALUES (?, ?, ?, ?, ?, 'h264', ?)`,
		id, f.ProjectID, f.ProjectID+"/exports/"+id+".mp4", resolution, ratio, day+"T09:00:00.000Z")
}

func projectState(f *chatfix.Fixture) string {
	f.T.Helper()
	p, err := f.Svc.Projects.Get(context.Background(), f.ProjectID)
	require.NoError(f.T, err)
	return string(p.State)
}

// countRows is the one number the query selects.
func countRows(f *chatfix.Fixture, q string, args ...any) int {
	f.T.Helper()
	var n int
	require.NoError(f.T, f.DB.Reader().QueryRow(q, args...).Scan(&n))
	return n
}
