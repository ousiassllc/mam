package bundlezip_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/bundlezip"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func voiceData(projectID, filePath string) domain.BundleData {
	voice, _ := json.Marshal(map[string]any{"file_path": filePath, "mime_type": "audio/wav", "language": "ja"})
	return domain.BundleData{ProjectID: projectID, Tables: []domain.BundleTable{
		{Name: "project", Rows: []domain.BundleRow{{"id": projectID}}},
		{Name: "character", Rows: []domain.BundleRow{{"id": "c1", "project_id": projectID, "name": "ユイ", "voice_reference": string(voice)}}},
	}}
}

func TestNewExport_RefusesAReferenceVoiceThatIsNotAFileOfTheProjectOrASharedOne(t *testing.T) {
	for _, bad := range []string{"../escape.wav", "other/../../etc/passwd", "/abs.wav", "c:/x.wav", "ep1/CON.wav"} {
		_, err := bundlezip.NewExport(voiceData("ep1", bad), t.TempDir(), time.Now())
		assert.Error(t, err, bad)
	}
	for _, good := range []string{"ep1/a.wav", "someone-else/a.wav"} {
		_, err := bundlezip.NewExport(voiceData("ep1", good), t.TempDir(), time.Now())
		assert.NoError(t, err, good)
	}
}

func TestOpen_RefusesABundleWhoseReferenceVoiceEscapesTheProjectDirectory(t *testing.T) {
	for _, bad := range []string{"../escape.wav", "other/a.wav", "ep1/../../x.wav"} {
		var zipped bytes.Buffer
		zw := zip.NewWriter(&zipped)
		now := time.Now()
		require.NoError(t, bundlezip.WriteJSON(zw, domain.BundleManifestName, now, domain.BundleManifest{Format: domain.BundleFormat, Version: domain.BundleVersion, ProjectID: "ep1", ExportedAt: now}))
		require.NoError(t, bundlezip.WriteJSON(zw, domain.BundleDataName, now, voiceData("ep1", bad)))
		require.NoError(t, zw.Close())

		_, err := bundlezip.Open(bytes.NewReader(zipped.Bytes()), int64(zipped.Len()))
		var invalid *bundlezip.InvalidError
		assert.ErrorAs(t, err, &invalid, bad)
	}
}
