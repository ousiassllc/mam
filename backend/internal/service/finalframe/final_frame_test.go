//go:build unix

// Package finalframe_test tests which Take file the Continue Agent is given a final frame of.
package finalframe_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/jobqueue"
	"github.com/ousiassllc/mam/backend/internal/agent/llm"
	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter"
	"github.com/ousiassllc/mam/backend/internal/media"
	"github.com/ousiassllc/mam/backend/internal/repository"
	"github.com/ousiassllc/mam/backend/internal/repository/testdb"
	"github.com/ousiassllc/mam/backend/internal/service"
	"github.com/ousiassllc/mam/backend/internal/testenv"
)

const proposeAnswer = `{"analysis":{"final_frame":"f","story":"s","character":"c","position":"p","emotion":"e","object":"","camera":"c","lighting":"l","motion":"m","dialogue":"d","music":"m"},"candidates":["a","b","c"]}`

// imageSpy answers Propose and keeps the images the Agent was given.
type imageSpy struct {
	mu     sync.Mutex
	images [][]llm.Image
}

func (s *imageSpy) Complete(_ context.Context, _ llm.Agent, req llm.Request) (llm.Response, error) {
	s.mu.Lock()
	s.images = append(s.images, req.Images)
	s.mu.Unlock()
	return llm.Response{Text: proposeAnswer, Provider: llm.ProviderAnthropic, Model: "fake"}, nil
}

// proposeFrom is Propose on a Shot whose only Take has the stored file path filePath; a real video
// is at outside.mp4 next to the assets directory, and one at p1/inside.mp4 inside it.
func proposeFrom(t *testing.T, filePath string) [][]llm.Image {
	t.Helper()
	tools, err := media.Look()
	if err != nil {
		testenv.MissingFFmpeg(t, "ffmpeg / ffprobe are not installed")
	}
	testenv.Parallel(t)
	dir := t.TempDir()
	assets := filepath.Join(dir, "assets")
	require.NoError(t, os.MkdirAll(filepath.Join(assets, "p1"), 0o750))
	video := testenv.PatternVideo(t, "320x240", 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "outside.mp4"), video, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(assets, "p1", "inside.mp4"), video, 0o600))

	db := testdb.Open(t)
	require.NoError(t, db.Write(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO project (id, mode, confirmation_level, production_template, quality_policy) VALUES ('p1', 'auto', 'full_auto', 'product_cm', 'balanced')`,
			`INSERT INTO brief (id, project_id) VALUES ('b1', 'p1')`,
			`INSERT INTO scene (id, project_id, title, order_index) VALUES ('sc1', 'p1', 'S', 0)`,
			`INSERT INTO shot (id, scene_id, objective, duration_seconds, order_index) VALUES ('sh1', 'sc1', 'run', 2, 0)`,
			`INSERT INTO prompt_version (id, shot_id, version_number, shot_spec_snapshot) VALUES ('pv1', 'sh1', 1, '{}')`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO take (id, prompt_version_id, model_name, model_parameters, file_path) VALUES ('t1', 'pv1', 'kling-3.0', '{}', ?)`, filePath)
		return err
	}))
	spy := &imageSpy{}
	jobs := jobqueue.New(repository.NewJobRepository(db), jobqueue.Options{})
	svc := service.New(service.Deps{DB: db, AssetsDir: assets, Media: &tools, LLM: spy, Jobs: jobs,
		Models: modeladapter.New(modeladapter.DefaultConfig(), modeladapter.Backends{})})
	t.Cleanup(svc.Close)

	_, _, err = svc.Continue.Propose(context.Background(), "sh1")

	require.NoError(t, err, "the Continue goes on with the text when there is no frame")
	spy.mu.Lock()
	defer spy.mu.Unlock()
	return spy.images
}

func TestContinue_TheAgentSeesTheFinalFrameOfATakeInsideTheAssetsDirectory(t *testing.T) {
	images := proposeFrom(t, "p1/inside.mp4")

	require.Len(t, images, 1)
	require.Len(t, images[0], 1)
	assert.True(t, strings.HasPrefix(string(images[0][0].Data), "\xff\xd8"), "a JPEG")
}

// A stored path that leaves the assets directory is not read: the final frame of a file outside
// would go to an external LLM.
func TestContinue_ATakeWhoseStoredPathLeavesTheAssetsDirectoryGivesNoFinalFrame(t *testing.T) {
	images := proposeFrom(t, "../outside.mp4")

	require.Len(t, images, 1)
	assert.Empty(t, images[0])
}
