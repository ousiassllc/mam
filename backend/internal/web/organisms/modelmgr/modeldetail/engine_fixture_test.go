package modeldetail_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr"
	"github.com/ousiassllc/mam/backend/internal/web/organisms/modelmgr/oob"
)

// The wtest static/src/components/poll/model-manager-refresh.wtest.ts (#962: the engine card asks for itself every 2 s while an install runs and has
// a refresh button; neither takes the modal that is open) draws these files with the real Tailwind CSS. They are what the components render:
// `MAM_UPDATE_FIXTURES=1 go test ./backend/internal/web/organisms/modelmgr/modeldetail` writes them again. This is a package of its own because
// the directories of the web layer are full (linterly).
func engine(updatedAt string) modelmgr.Engine {
	return modelmgr.Engine{
		Supported: true, Writable: true, Dir: "C:\\mam\\comfyui", Free: "120 GiB", UpdatedAt: updatedAt,
		Installed: true, Version: "0.3.10", PinnedVersion: "0.3.10", Variant: "nvidia", VariantLabel: "NVIDIA（CUDA）", State: "stopped",
	}
}

func TestEngineFixtures_AreWhatTheComponentsRender(t *testing.T) {
	installing := engine("10:00:00")
	installing.Installed = false
	installing.Install, installing.Job = &modelmgr.Progress{JobID: "job-1", Percent: 40, Message: "ダウンロード中", Bytes: "0.8 GiB / 2.0 GiB"}, "job-1"
	done := engine("10:00:05")
	done.Notice = "ComfyUI エンジンを導入しました"

	for name, c := range map[string]templ.Component{
		"mm-engine-installing.html": modelmgr.EngineCard(installing),
		"mm-engine-idle.html":       modelmgr.EngineCard(engine("10:00:00")),
		"mm-engine-done.html":       oob.EngineWithCatalog(done, modelmgr.Catalog{OOB: true}),
	} {
		var sb strings.Builder
		require.NoError(t, c.Render(context.Background(), &sb))
		file := filepath.Join("..", "..", "..", "..", "..", "..", "static", "src", "components", "testdata", name)
		if os.Getenv("MAM_UPDATE_FIXTURES") != "" {
			require.NoError(t, os.WriteFile(file, []byte(sb.String()), 0o600)) // #nosec G306 -- a test fixture in the repository
			continue
		}
		want, err := os.ReadFile(file) // #nosec G304 -- a fixed path inside the repository
		require.NoError(t, err, "write it with MAM_UPDATE_FIXTURES=1")
		assert.Equal(t, string(want), sb.String(), "%s: the component changed: write the fixture again with MAM_UPDATE_FIXTURES=1", name)
	}
}
